package server

import (
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/gortc/gortcd/internal/allocator"
	"github.com/gortc/stun"
	"github.com/gortc/turn"
)

// Server is RFC 5389 basic server implementation.
//
// Current implementation is UDP only and not ALTERNATE-SERVER.
// It does not support backwards compatibility with RFC 3489.
type Server struct {
	log    *zap.Logger
	allocs *allocator.Allocator
	conn   net.PacketConn
	auth   Auth
	close  chan struct{}
	wg     sync.WaitGroup
}

// Options is set of available options for Server.
type Options struct {
	Log         *zap.Logger
	Auth        Auth // no authentication if nil
	Conn        net.PacketConn
	CollectRate time.Duration
	ManualStart bool // don't start bg activity
}

// New initializes and returns new server from options.
func New(o Options) (*Server, error) {
	if o.Log == nil {
		o.Log = zap.NewNop()
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
		log:    o.Log,
		auth:   o.Auth,
		conn:   o.Conn,
		allocs: allocs,
		close:  make(chan struct{}),
	}
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
	t := time.NewTicker(rate)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case now := <-t.C:
				s.collect(now)
			case <-s.close: // pass
			default:
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
		}, ctx.time.Add(time.Minute*5), s,
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
	switch lifetime.Duration {
	case 0:
		s.allocs.Remove(ctx.client)
	default:
		t := ctx.time.Add(lifetime.Duration)
		if err := s.allocs.Refresh(ctx.client, allocator.Addr(addr), t); err != nil {
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
		if lifetime.Duration > time.Hour {
			// Requested lifetime is too big.
			return ctx.buildErr(stun.CodeBadRequest)
		}
	case stun.ErrAttributeNotFound:
		lifetime.Duration = time.Minute // default
	default:
		return errors.Wrap(err, "failed to get lifetime")
	}
	s.log.Info("processing create permission request")
	if err := s.allocs.CreatePermission(ctx.client, allocator.Addr(addr), ctx.time.Add(lifetime.Duration)); err != nil {
		return errors.Wrap(err, "failed to create allocation")
	}
	return ctx.buildOk()
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
	switch ctx.request.Type {
	case stun.BindingRequest:
		return s.processBindingRequest(ctx)
	case turn.AllocateRequest:
		return s.processAllocateRequest(ctx)
	case turn.CreatePermissionRequest:
		return s.processCreatePermissionRequest(ctx)
	case turn.RefreshRequest:
		return s.processRefreshRequest(ctx)
	case turn.SendIndication:
		return s.processSendIndication(ctx)
	default:
		s.log.Warn("unsupported request type")
		return ctx.buildErr(stun.CodeBadRequest)
	}
}

func (s *Server) serveConn(c net.PacketConn, ctx *context) error {
	if c == nil {
		return nil
	}
	buf := make([]byte, 1024)
	n, addr, err := c.ReadFrom(buf)
	if err != nil {
		s.log.Warn("readFrom failed", zap.Error(err))
		return nil
	}
	ctx.time = time.Now()
	ctx.request.Raw = buf[:n]
	s.log.Debug("read",
		zap.Int("n", n),
		zap.Stringer("addr", addr),
	)
	switch a := addr.(type) {
	case *net.UDPAddr:
		ctx.client.IP = a.IP
		ctx.client.Port = a.Port
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
	if err != nil {
		s.log.Warn("writeTo failed", zap.Error(err))
	}
	return err
}

// Serve reads packets from connections and responds to BINDING requests.
func (s *Server) Serve() error {
	var (
		ctx = &context{
			response: new(stun.Message),
			request:  new(stun.Message),
		}
	)
	for {
		if err := s.serveConn(s.conn, ctx); err != nil {
			s.log.Error("serveConn failed", zap.Error(err))
		}
		ctx.reset()
	}
}
