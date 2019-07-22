package server

import (
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-reuseport"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"gortc.io/gortcd/internal/allocator"
	"gortc.io/gortcd/internal/auth"
	"gortc.io/gortcd/internal/filter"
	"gortc.io/stun"
	"gortc.io/turn"
)

// Server is RFC 5389 basic server implementation.
//
// Current implementation is UDP only and not ALTERNATE-SERVER.
// It does not support backwards compatibility with RFC 3489.
type Server struct {
	addr        turn.Addr
	conns       []io.Closer
	conn        net.PacketConn
	auth        Auth
	nonce       NonceManager
	cfg         atomic.Value
	log         *zap.Logger
	allocs      *allocator.Allocator
	close       chan struct{}
	handlers    map[stun.MessageType]handleFunc
	pool        *workerPool
	wg          sync.WaitGroup
	reusePort   bool
	promMetrics *promMetrics
}

func (s *Server) config() config { return s.cfg.Load().(config) }

// setOptions updates subset of current server configuration.
//
// Currently supported:
//	* AuthForSTUN
//	* Software
//	* Realm
//	* PeerRule
//	* ClientRule
//	* DebugCollect
//	* MetricsEnabled
func (s *Server) setOptions(opt Options) { s.cfg.Store(s.newConfig(opt)) }

// Options is set of available options for Server.
type Options struct {
	Software       string // not adding SOFTWARE attribute if blank
	Realm          string
	Auth           Auth // no authentication if nil
	Conn           net.PacketConn
	Labels         prometheus.Labels // prometheus labels
	Registry       MetricsRegistry   // prometheus registry
	MetricsEnabled bool              // enable prometheus metrics (adds overhead)
	NonceManager   NonceManager      // optional nonce manager implementation
	PeerRule       filter.Rule
	ClientRule     filter.Rule // filtering rule for listeners
	Log            *zap.Logger
	CollectRate    time.Duration
	Workers        int           // maximum workers count
	NonceDuration  time.Duration // no nonce rotate if 0
	ManualStart    bool          // don't start bg activity
	AuthForSTUN    bool          // require auth for binding requests
	ReusePort      bool          // spawn more sockets on same port if available
	DebugCollect   bool          // debug collect calls
}

// Auth represents message authenticator.
type Auth interface {
	Auth(m *stun.Message) (stun.MessageIntegrity, error)
}

// NonceManager represents nonce manager (rotate and verify).
type NonceManager interface {
	Check(tuple turn.FiveTuple, value stun.Nonce, at time.Time) (stun.Nonce, error)
}

// MetricsRegistry represents prometheus metrics registry.
type MetricsRegistry interface {
	Register(c prometheus.Collector) error
}

// New initializes and returns new server from options.
func New(o Options) (*Server, error) {
	if o.Log == nil {
		o.Log = zap.NewNop()
	}
	if o.Workers == 0 {
		o.Workers = 100
	}
	if o.CollectRate == 0 {
		o.CollectRate = time.Second
	}
	if len(o.Labels) == 0 {
		o.Labels = prometheus.Labels{}
	}
	o.Labels["addr"] = o.Conn.LocalAddr().String()
	netAlloc, err := allocator.NewNetAllocator(o.Log.Named("port"), o.Conn.LocalAddr(), allocator.SystemPortAllocator{})
	if err != nil {
		return nil, err
	}
	allocs := allocator.NewAllocator(allocator.Options{
		Log:    o.Log.Named("allocator"),
		Conn:   netAlloc,
		Labels: o.Labels,
	})
	if o.NonceManager == nil {
		o.NonceManager = auth.NewNonceAuth(o.NonceDuration)
	}
	if o.PeerRule == nil {
		o.PeerRule = filter.AllowAll
	}
	if o.ClientRule == nil {
		o.ClientRule = filter.AllowAll
	}
	s := &Server{
		auth:        o.Auth,
		nonce:       o.NonceManager,
		conn:        o.Conn,
		allocs:      allocs,
		close:       make(chan struct{}),
		reusePort:   reuseport.Available() && o.ReusePort,
		promMetrics: newPromMetrics(o.Labels),
	}
	s.cfg.Store(s.newConfig(o))
	s.setHandlers()
	if a, ok := o.Conn.LocalAddr().(*net.UDPAddr); ok {
		s.addr.IP = a.IP
		s.addr.Port = a.Port
	} else {
		return nil, errors.New("unexpected local addr")
	}
	s.log = o.Log.With(zap.Stringer("server", s.addr))
	if !o.ManualStart {
		s.Start(o.CollectRate)
	}
	if o.Registry != nil {
		if err := o.Registry.Register(s.allocs); err != nil {
			return nil, errors.Wrap(err, "failed to register")
		}
		if err := o.Registry.Register(s.promMetrics); err != nil {
			return nil, errors.Wrap(err, "failed to register server metrics")
		}
	}
	s.pool = &workerPool{
		Logger:          s.log.Named("pool"),
		WorkerFunc:      s.serveConn,
		MaxWorkersCount: o.Workers,
	}
	return s, nil
}

// Start starts background activity.
func (s *Server) Start(rate time.Duration) { s.startCollect(rate) }

func (s *Server) startCollect(rate time.Duration) {
	s.wg.Add(1)
	s.log.Debug("started startCollect with rate", zap.Duration("rate", rate))
	t := time.NewTicker(rate)
	go func() {
		s.log.Debug("startCollect goroutine starting")
		defer func() {
			s.log.Debug("startCollect goroutine returned")
		}()
		defer s.wg.Done()
		for {
			select {
			case now := <-t.C:
				if s.config().debugCollect {
					s.log.Debug("collecting")
				}
				s.collect(now)
			case <-s.close:
				return
			}
		}
	}()
}

func (s *Server) collect(t time.Time) { s.allocs.Prune(t) }

// Close stops background activity.
func (s *Server) Close() error {
	// TODO(ar): Free resources.
	close(s.close)
	s.log.Debug("closing")
	s.pool.Stop()
	if err := s.conn.Close(); err != nil {
		s.log.Warn("failed to close connection", zap.Error(err))
	}
	for _, conn := range s.conns {
		if err := conn.Close(); err != nil {
			s.log.Warn("failed to close connection", zap.Error(err))
		}
	}
	s.wg.Wait()
	return nil
}

var errNotSTUNMessage = errors.New("not stun message")

func (s *Server) process(ctx *context) error {
	// Performing de-multiplexing of STUN and TURN's ChannelData messages.
	// The checks are ordered from faster to slower one.
	switch {
	case stun.IsMessage(ctx.request.Raw):
		ctx.cfg.metrics.incSTUNMessages()
		return s.processMessage(ctx)
	case turn.IsChannelData(ctx.request.Raw):
		return s.processChannelData(ctx)
	default:
		if ce := s.log.Check(zapcore.DebugLevel, "not looks like stun message"); ce != nil {
			ce.Write(zap.Stringer("addr", ctx.client))
		}
		return errNotSTUNMessage
	}
}

func (s *Server) serveConn(ctx *context) error {
	ctx.time = time.Now()
	ctx.request.Raw = ctx.buf
	ctx.cdata.Raw = ctx.buf
	switch a := ctx.addr.(type) {
	case *net.UDPAddr:
		ctx.client.FromUDPAddr(a)
		ctx.proto = turn.ProtoUDP
	default:
		s.log.Error("unknown addr", zap.Stringer("addr", ctx.addr))
		return errors.Errorf("unknown addr %s", ctx.addr)
	}
	if !ctx.allowClient(ctx.client) {
		if ce := s.log.Check(zapcore.DebugLevel, "client denied"); ce != nil {
			ce.Write(zap.Stringer("addr", ctx.client))
		}
		return nil
	}
	ctx.setTuple()
	if processErr := s.process(ctx); processErr != nil {
		if processErr != errNotSTUNMessage {
			s.log.Error("process failed", zap.Error(processErr))
		}
		return nil
	}
	if len(ctx.response.Raw) == 0 {
		// Indication.
		return nil
	}
	if setErr := ctx.conn.SetWriteDeadline(ctx.time.Add(time.Second)); setErr != nil {
		s.log.Warn("failed to set deadline", zap.Error(setErr))
	}
	_, writeErr := ctx.conn.WriteTo(ctx.response.Raw, ctx.addr)
	if writeErr != nil && !isErrConnClosed(writeErr) {
		s.log.Warn("writeTo failed", zap.Error(writeErr))
		return writeErr
	}
	return nil
}

func isErrConnClosed(err error) bool {
	return strings.HasSuffix(err.Error(), "use of closed network connection")
}

func (s *Server) worker(conn net.PacketConn) {
	defer s.wg.Done()
	s.log.Debug("worker started")
	defer s.log.Debug("worker done")
	buf := make([]byte, 2048)
	for {
		select {
		case <-s.close:
			return
		default:
			// pass
		}
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			if !isErrConnClosed(err) {
				s.log.Warn("readFrom failed", zap.Error(err))
			}
			break
		}

		// Preparing context.
		ctx := acquireContext()
		ctx.conn = conn
		ctx.buf = ctx.buf[:cap(ctx.buf)]
		copy(ctx.buf, buf)
		ctx.addr = addr
		ctx.buf = ctx.buf[:n]
		ctx.server = s.addr
		ctx.cfg = s.config()

		for i := 0; i < 7; i++ {
			if s.pool.Serve(ctx) {
				break
			}
			s.log.Warn("not enough workers")
			time.Sleep(time.Millisecond * 300)
		}
	}
}

func (s *Server) start() {
	s.pool.Start()
}

// Serve reads packets from connections and responds to BINDING requests.
func (s *Server) Serve() error {
	s.start()
	for i := 0; i < runtime.GOMAXPROCS(-1); i++ {
		s.wg.Add(1)
		if s.reusePort {
			s.log.Debug("reusing port for worker", zap.Int("w", i))
			laddr := s.conn.LocalAddr()
			conn, err := reuseport.ListenPacket(laddr.Network(), laddr.String())
			if err != nil {
				s.log.Warn("failed to listen for additional socket")
				conn = s.conn
			} else {
				s.conns = append(s.conns, conn)
			}
			go s.worker(conn)
		} else {
			go s.worker(s.conn)
		}
	}
	s.wg.Wait()
	return nil
}
