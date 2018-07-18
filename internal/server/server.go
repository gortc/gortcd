package server

import (
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"strings"

	"github.com/gortc/gortcd/internal/allocator"
	"github.com/gortc/stun"
	"github.com/gortc/turn"
)

// Server is RFC 5389 basic server implementation.
//
// Current implementation is UDP only and not ALTERNATE-SERVER.
// It does not support backwards compatibility with RFC 3489.
type Server struct {
	addr     allocator.Addr
	log      *zap.Logger
	allocs   *allocator.Allocator
	conn     net.PacketConn
	auth     Auth
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
	}
}

// Options is set of available options for Server.
type Options struct {
	Log         *zap.Logger
	Auth        Auth // no authentication if nil
	Conn        net.PacketConn
	CollectRate time.Duration
	ManualStart bool // don't start bg activity
	Workers     int
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
	netAlloc, err := allocator.NewNetAllocator(
		o.Log.Named("port"), o.Conn.LocalAddr(), allocator.SystemPortAllocator{},
	)
	if err != nil {
		return nil, err
	}
	allocs := allocator.NewAllocator(o.Log.Named("allocator"), netAlloc)
	s := &Server{
		auth:   o.Auth,
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
	return s, nil
}

// Auth represents message authenticator.
type Auth interface {
	Auth(m *stun.Message) (stun.MessageIntegrity, error)
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
	s.log.Info("started startCollect with rate", zap.Duration("rate", rate))
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
	s.log.Info("closing")
	s.conn.Close()
	s.wg.Wait()
	return nil
}

func (s *Server) collect(t time.Time) {
	s.allocs.Collect(t)
}

func (s *Server) sendByPermission(
	data turn.Data,
	client allocator.Addr,
	addr turn.PeerAddress,
) error {
	s.log.Info("searching for allocation",
		zap.Stringer("client", client),
		zap.Stringer("addr", addr),
	)
	_, err := s.allocs.Send(client, allocator.Addr(addr), data)
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
	l.Info("got peer data")
	if err := s.conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		l.Error("failed to SetWriteDeadline", zap.Error(err))
	}
	m := stun.New()
	if err := m.Build(
		stun.TransactionID,
		stun.NewType(stun.MethodData, stun.ClassIndication),
		turn.Data(d),
		stun.Fingerprint,
	); err != nil {
		l.Error("failed to build", zap.Error(err))
		return
	}
	if _, err := s.conn.WriteTo(m.Raw, destination); err != nil {
		l.Error("failed to write", zap.Error(err))
	}
	l.Info("sent data from peer", zap.Stringer("m", m))
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
	relayedAddr, err := s.allocs.New(
		allocator.FiveTuple{
			Server: ctx.server,
			Client: ctx.client,
			Proto:  transport.Protocol,
		}, ctx.time.Add(s.cfg.DefaultLifetime()), s,
	)
	switch err {
	case nil:
		return ctx.buildOk(
			(*stun.XORMappedAddress)(&ctx.client),
			(*turn.RelayedAddress)(&relayedAddr),
		)
	case allocator.ErrAllocationMismatch:
		return ctx.buildErr(stun.CodeAllocMismatch)
	default:
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
	tuple := allocator.FiveTuple{
		Server: s.addr,
		Client: ctx.client,
		Proto:  turn.ProtoUDP, // TODO: fill from request
	}
	switch lifetime.Duration {
	case 0:
		s.allocs.Remove(tuple)
	default:
		t := ctx.time.Add(lifetime.Duration)
		if err := s.allocs.Refresh(tuple, allocator.Addr(addr), t); err != nil {
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
		return errors.Wrap(err, "failed to ger create permission request addr")
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
	s.log.Info("processing create permission request")
	if err := s.allocs.CreatePermission(ctx.client, allocator.Addr(addr), ctx.time.Add(lifetime.Duration)); err != nil {
		return errors.Wrap(err, "failed to create allocation")
	}
	return ctx.buildOk(&lifetime)
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
	s.log.Info("sending data", zap.Stringer("to", addr))
	if err := s.sendByPermission(data, ctx.client, addr); err != nil {
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
	return ctx.request.Type != stun.BindingRequest
}

var (
	realm        = stun.NewRealm("realm")
	defaultNonce = stun.NewNonce("nonce")
)

func (s *Server) process(ctx *context) error {
	if !stun.IsMessage(ctx.request.Raw) {
		if ce := s.log.Check(zapcore.DebugLevel, "not looks like stun message"); ce != nil {
			ce.Write(zap.Stringer("addr", ctx.client))
		}
		return errNotSTUNMessage
	}
	if err := ctx.request.Decode(); err != nil {
		if ce := s.log.Check(zapcore.DebugLevel, "failed to decode request"); ce != nil {
			ce.Write(zap.Stringer("addr", ctx.client), zap.Error(err))
		}
		return nil
	}
	ctx.software = software
	ctx.realm = realm
	ctx.nonce = defaultNonce
	if ce := s.log.Check(zapcore.InfoLevel, "got message"); ce != nil {
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
		switch integrity, err := s.auth.Auth(ctx.request); err {
		case stun.ErrAttributeNotFound:
			if ce := s.log.Check(zapcore.DebugLevel, "integrity required"); ce != nil {
				ce.Write(zap.Stringer("addr", ctx.client), zap.Stringer("req", ctx.request))
			}
			return ctx.buildErr(stun.CodeUnauthorised)
		case nil:
			ctx.integrity = integrity
		default:
			if ce := s.log.Check(zapcore.DebugLevel, "failed to auth"); ce != nil {
				ce.Write(zap.Stringer("addr", ctx.client), zap.Stringer("req", ctx.request),
					zap.Error(err),
				)
			}
			return ctx.buildErr(stun.CodeWrongCredentials)
		}
	}
	// Selecting handler based on request message type.
	h, ok := s.handlers[ctx.request.Type]
	if ok {
		return h(ctx)
	}
	s.log.Warn("unsupported request type")
	return ctx.buildErr(stun.CodeBadRequest)
}

func (s *Server) serveConn(c net.PacketConn, ctx *context) error {
	n, addr, err := c.ReadFrom(ctx.buf)
	if err != nil {
		if !isErrConnClosed(err) {
			s.log.Warn("readFrom failed", zap.Error(err))
		}
		return nil
	}
	ctx.server = s.addr
	ctx.time = time.Now()
	ctx.request.Raw = ctx.buf[:n]
	if ce := s.log.Check(zapcore.DebugLevel, "read"); ce != nil {
		ce.Write(
			zap.Int("n", n),
			zap.Stringer("addr", addr),
		)
	}

	switch a := addr.(type) {
	case *net.UDPAddr:
		ctx.client.FromUDPAddr(a)
	default:
		s.log.Error("unknown addr", zap.Stringer("addr", addr))
		return errors.Errorf("unknown addr %s", addr)
	}
	if err = s.process(ctx); err != nil {
		if err == errNotSTUNMessage {
			return nil
		}
		s.log.Error("process failed", zap.Error(err))
		return nil
	}
	if len(ctx.response.Raw) == 0 {
		// Indication.
		return nil
	}
	c.SetWriteDeadline(ctx.time.Add(time.Second))
	_, err = c.WriteTo(ctx.response.Raw, addr)
	if err != nil && !isErrConnClosed(err) {
		s.log.Warn("writeTo failed", zap.Error(err))
		return err
	}
	return nil
}

func isErrConnClosed(err error) bool {
	return strings.HasSuffix(err.Error(), "use of closed network connection")
}

func (s *Server) worker() {
	defer s.wg.Done()
	s.log.Info("worker started")
	defer s.log.Info("worker done")
	var (
		ctx = &context{
			response: new(stun.Message),
			request:  new(stun.Message),
			buf:      make([]byte, 2048),
		}
	)
	for {
		if err := s.serveConn(s.conn, ctx); err != nil {
			s.log.Error("serveConn failed", zap.Error(err))
		}
		ctx.reset()
		select {
		case <-s.close:
			return
		default:
			// pass
		}
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
