package server

import (
	"net"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/gortc/gortcd/internal/allocator"
	"github.com/gortc/gortcd/internal/auth"
	"github.com/gortc/stun"
	"github.com/gortc/turn"
)

// Server is RFC 5389 basic server implementation.
//
// Current implementation is UDP only and not ALTERNATE-SERVER.
// It does not support backwards compatibility with RFC 3489.
type Server struct {
	realm    stun.Realm
	addr     allocator.Addr
	log      *zap.Logger
	allocs   *allocator.Allocator
	conn     net.PacketConn
	auth     Auth
	nonce    NonceManager
	close    chan struct{}
	wg       sync.WaitGroup
	handlers map[stun.MessageType]handleFunc
	cfg      *config
}

type handleFunc = func(ctx *context) error

func (s *Server) setHandlers() {
	s.handlers = map[stun.MessageType]handleFunc{
		stun.BindingRequest:          s.processBindingRequest,
		turn.AllocateRequest:         s.processAllocateRequest,
		turn.CreatePermissionRequest: s.processCreatePermissionRequest,
		turn.RefreshRequest:          s.processRefreshRequest,
		turn.SendIndication:          s.processSendIndication,
		channelBindRequest:           s.processChannelBinding,
	}
}

var channelBindRequest = stun.NewType(stun.MethodChannelBind, stun.ClassRequest)

// Options is set of available options for Server.
type Options struct {
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
	s := &Server{
		realm:  stun.NewRealm(o.Realm),
		auth:   o.Auth,
		nonce:  auth.NewNonceAuth(o.NonceDuration),
		conn:   o.Conn,
		allocs: allocs,
		close:  make(chan struct{}),
		cfg:    newConfig(o),
	}
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

// Auth represents message authenticator.
type Auth interface {
	Auth(m *stun.Message) (stun.MessageIntegrity, error)
}

// NonceManager represents nonce manager (rotate and verify).
type NonceManager interface {
	Check(tuple allocator.FiveTuple, value stun.Nonce, at time.Time) (stun.Nonce, error)
}

var (
	software          = stun.NewSoftware("gortc/gortcd")
	errNotSTUNMessage = errors.New("not stun message")
)

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

func (s *Server) collect(t time.Time) {
	s.allocs.Prune(t)
}

func (s *Server) sendByBinding(ctx *context, n turn.ChannelNumber, data []byte) error {
	s.log.Debug("searching for allocation via binding",
		zap.Stringer("tuple", ctx.tuple),
		zap.Stringer("n", ctx.cdata.Number),
	)
	_, err := s.allocs.SendBound(ctx.tuple, n, data)
	return err
}

func (s *Server) sendByPermission(
	ctx *context,
	addr allocator.Addr,
	data []byte,
) error {
	s.log.Debug("searching for allocation",
		zap.Stringer("tuple", ctx.tuple),
		zap.Stringer("addr", addr),
	)
	_, err := s.allocs.Send(ctx.tuple, addr, data)
	return err
}

// HandlePeerData implements allocator.PeerHandler.
func (s *Server) HandlePeerData(d []byte, t allocator.FiveTuple, a allocator.Addr) {
	destination := &net.UDPAddr{
		IP:   t.Client.IP,
		Port: t.Client.Port,
	}
	l := s.log.With(
		zap.Stringer("t", t),
		zap.Stringer("addr", a),
		zap.Int("len", len(d)),
		zap.Stringer("d", destination),
	)
	l.Debug("got peer data")
	if err := s.conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		l.Error("failed to SetWriteDeadline", zap.Error(err))
	}
	if n, err := s.allocs.Bound(t, a); err == nil {
		// Using channel data.
		d := turn.ChannelData{
			Number: n,
			Data:   d,
		}
		d.Encode()
		if _, err := s.conn.WriteTo(d.Raw, destination); err != nil {
			l.Error("failed to write", zap.Error(err))
		}
		l.Debug("sent data via channel", zap.Stringer("n", n))
		return
	}
	m := stun.New()
	if err := m.Build(
		stun.TransactionID,
		stun.NewType(stun.MethodData, stun.ClassIndication),
		turn.Data(d), turn.PeerAddress(a),
		stun.Fingerprint,
	); err != nil {
		l.Error("failed to build", zap.Error(err))
		return
	}
	if _, err := s.conn.WriteTo(m.Raw, destination); err != nil {
		l.Error("failed to write", zap.Error(err))
	}
	l.Debug("sent data from peer", zap.Stringer("m", m))
}

func (s *Server) processBindingRequest(ctx *context) error {
	return ctx.buildOk(
		(*stun.XORMappedAddress)(&ctx.client),
	)
}

func (s *Server) processAllocateRequest(ctx *context) error {
	var (
		transport turn.RequestedTransport
	)
	if err := transport.GetFrom(ctx.request); err != nil {
		return ctx.buildErr(stun.CodeBadRequest)
	}
	lifetime := s.cfg.DefaultLifetime()
	relayedAddr, err := s.allocs.New(ctx.tuple, ctx.time.Add(lifetime), s)
	switch err {
	case nil:
		return ctx.buildOk(
			(*stun.XORMappedAddress)(&ctx.tuple.Client),
			(*turn.RelayedAddress)(&relayedAddr),
			turn.Lifetime{Duration: lifetime},
		)
	case allocator.ErrAllocationMismatch:
		return ctx.buildErr(stun.CodeAllocMismatch)
	default:
		s.log.Warn("failed to allocate", zap.Error(err))
		return ctx.buildErr(stun.CodeServerError)
	}
}

func (s *Server) processRefreshRequest(ctx *context) error {
	var (
		addr     turn.PeerAddress
		lifetime turn.Lifetime
	)
	if err := ctx.request.Parse(&addr); err != nil && err != stun.ErrAttributeNotFound {
		return errors.Wrap(err, "failed to parse refresh request")
	}
	if err := ctx.request.Parse(&lifetime); err != nil {
		if err != stun.ErrAttributeNotFound {
			return errors.Wrap(err, "failed to parse")
		}
	}
	switch lifetime.Duration {
	case 0:
		s.allocs.Remove(ctx.tuple)
	default:
		var (
			peer    = allocator.Addr(addr)
			timeout = ctx.time.Add(lifetime.Duration)
		)
		if err := s.allocs.Refresh(ctx.tuple, peer, timeout); err != nil {
			s.log.Error("failed to refresh allocation", zap.Error(err))
			return ctx.buildErr(stun.CodeServerError)
		}
	}
	return ctx.buildOk()
}

func (s *Server) processCreatePermissionRequest(ctx *context) error {
	var (
		addr     turn.PeerAddress
		lifetime turn.Lifetime
	)
	if err := addr.GetFrom(ctx.request); err != nil {
		return errors.Wrap(err, "failed to get create permission request addr")
	}
	switch err := lifetime.GetFrom(ctx.request); err {
	case nil:
		max := s.cfg.MaxLifetime()
		if lifetime.Duration > max {
			lifetime.Duration = max
		}
	case stun.ErrAttributeNotFound:
		lifetime.Duration = s.cfg.DefaultLifetime()
	default:
		return errors.Wrap(err, "failed to get lifetime")
	}
	s.log.Debug("processing create permission request")
	var (
		peer    = allocator.Addr(addr)
		timeout = ctx.time.Add(lifetime.Duration)
	)
	switch err := s.allocs.CreatePermission(ctx.tuple, peer, timeout); err {
	case allocator.ErrAllocationMismatch:
		return ctx.buildErr(stun.CodeAllocMismatch)
	case nil:
		return ctx.buildOk(&lifetime)
	default:
		return errors.Wrap(err, "failed to create allocation")
	}
}

func (s *Server) processSendIndication(ctx *context) error {
	var (
		data turn.Data
		addr turn.PeerAddress
	)
	if err := ctx.request.Parse(&data, &addr); err != nil {
		s.log.Error("failed to parse send indication", zap.Error(err))
		return errors.Wrap(err, "failed to parse send indication")
	}
	s.log.Debug("sending data", zap.Stringer("to", addr))
	if err := s.sendByPermission(ctx, allocator.Addr(addr), data); err != nil {
		s.log.Warn("send failed",
			zap.Error(err),
		)
	}
	return nil
}

func (s *Server) needAuth(ctx *context) bool {
	if s.auth == nil {
		return false
	}
	if ctx.request.Type.Class == stun.ClassIndication {
		return false
	}
	if ctx.request.Type == stun.BindingRequest && !s.cfg.RequireAuthForSTUN() {
		return false
	}
	return true
}

func (s *Server) processChannelBinding(ctx *context) error {
	var (
		addr   turn.PeerAddress
		number turn.ChannelNumber
	)
	if parseErr := ctx.request.Parse(&addr, &number); parseErr != nil {
		s.log.Debug("channel binding parse failed", zap.Error(parseErr))
		return ctx.buildErr(stun.CodeBadRequest)
	}
	var (
		peer     = allocator.Addr(addr)
		lifetime = s.cfg.DefaultLifetime()
		timeout  = ctx.time.Add(lifetime)
	)
	switch err := s.allocs.ChannelBind(ctx.tuple, number, peer, timeout); err {
	case allocator.ErrAllocationMismatch:
		return ctx.buildErr(stun.CodeAllocMismatch)
	case nil:
		return ctx.buildOk(&number, &turn.Lifetime{Duration: lifetime})
	default:
		return errors.Wrap(err, "failed to create allocation")
	}
}

func (s *Server) processChannelData(ctx *context) error {
	if err := ctx.cdata.Decode(); err != nil {
		if ce := s.log.Check(zapcore.DebugLevel, "failed to decode channel data"); ce != nil {
			ce.Write(zap.Stringer("addr", ctx.client), zap.Error(err))
		}
		return nil
	}
	if ce := s.log.Check(zapcore.DebugLevel, "got channel data"); ce != nil {
		ce.Write(
			zap.Int("channel", int(ctx.cdata.Number)),
			zap.Int("len", ctx.cdata.Length),
		)
	}
	return s.sendByBinding(ctx, ctx.cdata.Number, ctx.cdata.Data)
}

func (s *Server) processMessage(ctx *context) error {
	if err := ctx.request.Decode(); err != nil {
		if ce := s.log.Check(zapcore.DebugLevel, "failed to decode request"); ce != nil {
			ce.Write(zap.Stringer("addr", ctx.client), zap.Error(err))
		}
		return nil
	}
	ctx.software = software
	ctx.realm = s.realm
	if ce := s.log.Check(zapcore.DebugLevel, "got message"); ce != nil {
		ce.Write(zap.Stringer("m", ctx.request), zap.Stringer("addr", ctx.client))
	}
	if ctx.request.Contains(stun.AttrFingerprint) {
		// Check fingerprint if provided.
		if err := stun.Fingerprint.Check(ctx.request); err != nil {
			s.log.Debug("fingerprint check failed", zap.Error(err))
			return ctx.buildErr(stun.CodeBadRequest)
		}
	}
	if s.needAuth(ctx) {
		// Getting nonce.
		nonceGetErr := ctx.nonce.GetFrom(ctx.request)
		if nonceGetErr != nil && nonceGetErr != stun.ErrAttributeNotFound {
			return ctx.buildErr(stun.CodeBadRequest)
		}
		validNonce, nonceErr := s.nonce.Check(ctx.tuple, ctx.nonce, ctx.time)
		if nonceErr != nil && nonceErr != auth.ErrStaleNonce {
			s.log.Error("nonce error", zap.Error(nonceErr))
			return ctx.buildErr(stun.CodeServerError)
		}
		ctx.nonce = validNonce
		// Check if client is trying to get nonce and realm.
		_, integrityAttrErr := ctx.request.Get(stun.AttrMessageIntegrity)
		if integrityAttrErr == stun.ErrAttributeNotFound {
			if ce := s.log.Check(zapcore.DebugLevel, "integrity required"); ce != nil {
				ce.Write(zap.Stringer("addr", ctx.client), zap.Stringer("req", ctx.request))
			}
			return ctx.buildErr(stun.CodeUnauthorised)
		}
		if nonceErr == auth.ErrStaleNonce {
			return ctx.buildErr(stun.CodeStaleNonce)
		}
		switch integrity, err := s.auth.Auth(ctx.request); err {
		case nil:
			ctx.integrity = integrity
		default:
			if ce := s.log.Check(zapcore.DebugLevel, "failed to auth"); ce != nil {
				ce.Write(zap.Stringer("addr", ctx.client), zap.Stringer("req", ctx.request),
					zap.Error(err),
				)
			}
			return ctx.buildErr(stun.CodeUnauthorised)
		}
	}
	// Selecting handler based on request message type.
	h, ok := s.handlers[ctx.request.Type]
	if ok {
		return h(ctx)
	}
	s.log.Warn("unsupported request type", zap.Stringer("t", ctx.request.Type))
	return ctx.buildErr(stun.CodeBadRequest)
}

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

var contextPool = &sync.Pool{
	New: func() interface{} {
		return &context{
			cdata:    new(turn.ChannelData),
			response: new(stun.Message),
			request:  new(stun.Message),
			buf:      make([]byte, 2048),
		}
	},
}

func acquireContext() *context {
	return contextPool.Get().(*context)
}

func putContext(ctx *context) {
	ctx.reset()
	contextPool.Put(ctx)
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
	for i := 0; i < s.cfg.Workers(); i++ {
		s.wg.Add(1)
		go s.worker()
	}
	s.wg.Wait()
	return nil
}
