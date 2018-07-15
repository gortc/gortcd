package server

import (
	"net"
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
// Current implementation is UDP only and not utilizes FINGERPRINT mechanism,
// nor ALTERNATE-SERVER, nor credentials mechanisms. It does not support
// backwards compatibility with RFC 3489.
type Server struct {
	log    *zap.Logger
	allocs *allocator.Allocator
	conn   net.PacketConn
	auth   Auth
}

type Options struct {
	Log  *zap.Logger
	Auth Auth
	Conn net.PacketConn
}

func New(o Options) (*Server, error) {
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
	}
	return s, nil
}

type Auth interface {
	Auth(m *stun.Message) (stun.MessageIntegrity, error)
}

var (
	software          = stun.NewSoftware("gortc/gortcd")
	errNotSTUNMessage = errors.New("not stun message")
)

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

type context struct {
	time      time.Time
	client    allocator.Addr
	request   *stun.Message
	response  *stun.Message
	nonce     stun.Nonce
	realm     stun.Realm
	integrity stun.MessageIntegrity
	software  stun.Software
}

func (c *context) reset() {
	c.time = time.Time{}
	c.client = allocator.Addr{}
	c.request.Reset()
	c.response.Reset()
	c.nonce = c.nonce[:0]
	c.realm = c.realm[:0]
	c.integrity = nil
	c.software = c.software[:0]
}

func (c *context) apply(s ...stun.Setter) error {
	for _, a := range s {
		if err := a.AddTo(c.response); err != nil {
			return err
		}
	}
	return nil
}

func (c *context) buildErr(s ...stun.Setter) error {
	return c.build(stun.ClassErrorResponse, c.request.Type.Method, s...)
}

func (c *context) buildOk(s ...stun.Setter) error {
	return c.build(stun.ClassSuccessResponse, c.request.Type.Method, s...)
}

func (c *context) build(class stun.MessageClass, method stun.Method, s ...stun.Setter) error {
	if c.request.Type.Class == stun.ClassIndication {
		// No responses for indication.
		return nil
	}
	c.response.Reset()
	c.response.Type.Class = class
	c.response.Type.Method = method
	c.response.WriteHeader()
	copy(c.response.TransactionID[:], c.request.TransactionID[:])
	if err := c.apply(&c.nonce, &c.realm); err != nil {
		return err
	}
	if len(c.software) > 0 {
		if err := c.software.AddTo(c.response); err != nil {
			return err
		}
	}
	if err := c.apply(s...); err != nil {
		return err
	}
	if len(c.integrity) > 0 {
		if err := c.integrity.AddTo(c.response); err != nil {
			return err
		}
	}
	return stun.Fingerprint.AddTo(c.response)
}

func (s *Server) processAllocateRequest(ctx *context) error {
	var (
		transport turn.RequestedTransport
	)
	if err := transport.GetFrom(ctx.request); err != nil {
		return ctx.buildErr(stun.CodeBadRequest)
	}
	server, err := s.allocs.New(
		ctx.client, transport.Protocol, s,
	)
	if err != nil {
		s.log.Error("failed to allocate", zap.Error(err))
		return ctx.buildErr(stun.CodeServerError)
	}
	return ctx.buildOk(
		(*stun.XORMappedAddress)(&server),
		(*turn.RelayedAddress)(&ctx.client),
	)
}

func (s *Server) processRefreshRequest(ctx *context) error {
	var (
		addr     turn.PeerAddress
		lifetime turn.Lifetime
	)
	if err := ctx.request.Parse(&addr); err != nil && err != stun.ErrAttributeNotFound {
		return errors.Wrap(err, "failed to parse refresh request")
	}
	if err := ctx.request.Parse(&addr); err != nil {
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
		return errors.Wrap(err, "failed to parse send indication")
	}
	if err := s.sendByPermission(data, ctx.client, addr); err != nil {
		s.log.Warn("send failed",
			zap.Error(err),
		)
	}
	return nil
}

func (s *Server) needAuth(ctx *context) bool {
	return ctx.request.Type != stun.BindingRequest
}

var (
	realm        = stun.NewRealm("realm")
	defaultNonce = stun.NewNonce("nonce")
)

func (s *Server) process(ctx *context) error {
	if !stun.IsMessage(ctx.request.Raw) {
		s.log.Debug("not looks like stun message", zap.Stringer("addr", ctx.client))
		return errNotSTUNMessage
	}
	if err := ctx.request.Decode(); err != nil {
		return errors.Wrap(err, "failed to read message")
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
			return ctx.buildErr(stun.CodeUnauthorised)
		case nil:
			ctx.integrity = integrity
		default:
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
	_, err = c.WriteTo(ctx.response.Raw, addr)
	if err != nil {
		s.log.Warn("writeTo failed", zap.Error(err))
	}
	return err
}

// Serve reads packets from connections and responds to BINDING requests.
func (s *Server) Serve() error {
	var (
		ctx = new(context)
	)
	for {
		ctx.time = time.Now()
		if err := s.serveConn(s.conn, ctx); err != nil {
			s.log.Error("serveConn failed", zap.Error(err))
		}
		ctx.reset()
	}
}
