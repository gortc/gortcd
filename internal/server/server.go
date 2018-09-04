package server

import (
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/gortc/gortcd/internal/allocator"
	"github.com/gortc/gortcd/internal/auth"
	"github.com/gortc/gortcd/internal/filter"
	"github.com/gortc/stun"
	"github.com/gortc/turn"
)

// Server is RFC 5389 basic server implementation.
//
// Current implementation is UDP only and not ALTERNATE-SERVER.
// It does not support backwards compatibility with RFC 3489.
type Server struct {
	addr     turn.Addr
	log      *zap.Logger
	allocs   *allocator.Allocator
	conn     net.PacketConn
	auth     Auth
	nonce    NonceManager
	close    chan struct{}
	wg       sync.WaitGroup
	handlers map[stun.MessageType]handleFunc
	cfg      atomic.Value
}

func (s *Server) config() config {
	return s.cfg.Load().(config)
}

// setOptions updates subset of current server configuration.
//
// Currently supported:
//	* AuthForSTUN
//	* Software
//	* Realm
//	* PeerRule
//	* ClientRule
func (s *Server) setOptions(opt Options) {
	s.cfg.Store(newConfig(opt))
}

// Options is set of available options for Server.
type Options struct {
	Software      string // not adding SOFTWARE attribute if blank
	Realm         string
	Log           *zap.Logger
	Auth          Auth // no authentication if nil
	Conn          net.PacketConn
	CollectRate   time.Duration
	ManualStart   bool // don't start bg activity
	AuthForSTUN   bool // require auth for binding requests
	Workers       int
	Registry      MetricsRegistry
	Labels        prometheus.Labels
	NonceDuration time.Duration // no nonce rotate if 0
	NonceManager  NonceManager  // optional nonce manager implementation
	PeerRule      filter.Rule
	ClientRule    filter.Rule // filtering rule for listeners
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
		o.Workers = runtime.GOMAXPROCS(0)
	}
	if o.CollectRate == 0 {
		o.CollectRate = time.Second
	}
	if len(o.Labels) == 0 {
		o.Labels = prometheus.Labels{}
	}
	o.Labels["addr"] = o.Conn.LocalAddr().String()
	netAlloc, err := allocator.NewNetAllocator(
		o.Log.Named("port"), o.Conn.LocalAddr(), allocator.SystemPortAllocator{},
	)
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
		auth:   o.Auth,
		nonce:  o.NonceManager,
		conn:   o.Conn,
		allocs: allocs,
		close:  make(chan struct{}),
	}
	s.cfg.Store(newConfig(o))
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
	}
	return s, nil
}

// Start starts background activity.
func (s *Server) Start(rate time.Duration) {
	s.startCollect(rate)
}

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
				s.log.Debug("collecting")
				s.collect(now)
			case <-s.close:
				return
			}
		}
	}()
}

func (s *Server) collect(t time.Time) {
	s.allocs.Prune(t)
}

// Close stops background activity.
func (s *Server) Close() error {
	// TODO(ar): Free resources.
	close(s.close)
	s.log.Debug("closing")
	if err := s.conn.Close(); err != nil {
		s.log.Warn("failed to close connection", zap.Error(err))
	}
	s.wg.Wait()
	return nil
}

var (
	errNotSTUNMessage = errors.New("not stun message")
)

func (s *Server) process(ctx *context) error {
	// Performing de-multiplexing of STUN and TURN's ChannelData messages.
	// The checks are ordered from faster to slower one.
	switch {
	case stun.IsMessage(ctx.request.Raw):
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

func (s *Server) serveConn(addr net.Addr, ctx *context) error {
	ctx.time = time.Now()
	ctx.request.Raw = ctx.buf
	ctx.cdata.Raw = ctx.buf
	switch a := addr.(type) {
	case *net.UDPAddr:
		ctx.client.FromUDPAddr(a)
		ctx.proto = turn.ProtoUDP
	default:
		s.log.Error("unknown addr", zap.Stringer("addr", addr))
		return errors.Errorf("unknown addr %s", addr)
	}
	if !ctx.allowClient(ctx.client) {
		if ce := s.log.Check(zapcore.DebugLevel, "client denied"); ce != nil {
			ce.Write(
				zap.Stringer("addr", ctx.client),
			)
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
	if setErr := s.conn.SetWriteDeadline(ctx.time.Add(time.Second)); setErr != nil {
		s.log.Warn("failed to set deadline", zap.Error(setErr))
	}
	_, writeErr := s.conn.WriteTo(ctx.response.Raw, addr)
	if writeErr != nil && !isErrConnClosed(writeErr) {
		s.log.Warn("writeTo failed", zap.Error(writeErr))
		return writeErr
	}
	return nil
}

func isErrConnClosed(err error) bool {
	return strings.HasSuffix(err.Error(), "use of closed network connection")
}

func (s *Server) worker() {
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
		n, addr, err := s.conn.ReadFrom(buf)
		if err != nil {
			if !isErrConnClosed(err) {
				s.log.Warn("readFrom failed", zap.Error(err))
			}
			break
		}
		// Preparing context.
		ctx := acquireContext()
		ctx.buf = ctx.buf[:cap(ctx.buf)]
		copy(ctx.buf, buf)
		ctx.buf = ctx.buf[:n]
		ctx.server = s.addr
		ctx.cfg = s.config()
		// Spawning serve goroutine.
		go func(clientAddr net.Addr, context *context) {
			if serveErr := s.serveConn(clientAddr, context); serveErr != nil {
				s.log.Error("serveConn failed", zap.Error(serveErr))
			}
			putContext(context)
		}(addr, ctx)
	}
}

// Serve reads packets from connections and responds to BINDING requests.
func (s *Server) Serve() error {
	cfg := s.config()
	for i := 0; i < cfg.workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}
	s.wg.Wait()
	return nil
}
