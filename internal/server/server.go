package server

import (
	"net"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

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

var (
	bindingSuccess = stun.NewType(stun.MethodBinding, stun.ClassSuccessResponse)
	allocSuccess   = stun.NewType(stun.MethodAllocate, stun.ClassSuccessResponse)
)

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

func (s *Server) processBindingRequest(addr allocator.Addr, req, res *stun.Message) error {
	return res.Build(req, bindingSuccess,
		software,
		(*stun.XORMappedAddress)(&addr),
		stun.Fingerprint,
	)
}

type context struct {
	client allocator.Addr
	req    *stun.Message
	res    *stun.Message
	nonce  stun.Nonce
	realm  stun.Realm
}

func (s *Server) processAllocateRequest(ctx context) error {
	var (
		req   = ctx.req
		res   = ctx.res
		realm = ctx.realm
		nonce = ctx.nonce
	)
	var (
		transport turn.RequestedTransport
	)
	if err := transport.GetFrom(ctx.req); err != nil {
		return res.Build(req, stun.NewType(stun.MethodAllocate, stun.ClassErrorResponse),
			stun.CodeBadRequest,
			nonce, realm, stun.Fingerprint,
		)
	}
	integrity, err := s.auth.Auth(ctx.req)
	if err != nil {
		return ctx.res.Build(ctx.req, stun.NewType(stun.MethodAllocate, stun.ClassErrorResponse),
			stun.CodeUnauthorised,
			nonce, realm, stun.Fingerprint,
		)
	}
	server, err := s.allocs.New(
		ctx.client, transport.Protocol, s,
	)
	if err != nil {
		s.log.Error("failed to allocate", zap.Error(err))
		return res.Build(req, stun.NewType(stun.MethodAllocate, stun.ClassErrorResponse),
			stun.CodeServerError,
			nonce, realm, integrity, stun.Fingerprint,
		)
	}
	return res.Build(req, allocSuccess,
		(*stun.XORMappedAddress)(&server),
		(*turn.RelayedAddress)(&ctx.client),
		nonce, realm, integrity, stun.Fingerprint,
	)
}

func (s *Server) process(addr net.Addr, b []byte, req, res *stun.Message) error {
	var (
		nonce       = stun.NewNonce("nonce")
		serverRealm = stun.NewRealm("realm")
	)
	if !stun.IsMessage(b) {
		s.log.Debug("not looks like stun message", zap.Stringer("addr", addr))
		return errNotSTUNMessage
	}
	if _, err := req.Write(b); err != nil {
		return errors.Wrap(err, "failed to read message")
	}
	var (
		ip   net.IP
		port int
	)
	switch a := addr.(type) {
	case *net.UDPAddr:
		ip = a.IP
		port = a.Port
	default:
		s.log.Error("unknown addr", zap.Stringer("addr", addr))
		return errors.Errorf("unknown addr %s", addr)
	}
	client := allocator.Addr{
		Port: port,
		IP:   ip,
	}
	now := time.Now()
	s.log.Info("got message",
		zap.Stringer("m", req),
		zap.Stringer("addr", client),
	)
	ctx := context{
		client: client,
		res:    res,
		req:    req,
		realm:  serverRealm,
		nonce:  nonce,
	}
	switch req.Type {
	case stun.BindingRequest:
		return s.processBindingRequest(client, req, res)
	case turn.AllocateRequest:
		return s.processAllocateRequest(ctx)
	case turn.CreatePermissionRequest:
		var (
			addr     turn.PeerAddress
			lifetime turn.Lifetime
		)
		if err := addr.GetFrom(req); err != nil {
			return errors.Wrap(err, "failed to ger create permission request addr")
		}
		switch err := lifetime.GetFrom(req); err {
		case nil:
			if lifetime.Duration > time.Hour {
				// Requested lifetime is too big.
				return res.Build(req, stun.NewType(stun.MethodCreatePermission, stun.ClassErrorResponse),
					stun.CodeBadRequest,
					stun.Fingerprint,
				)
			}
		case stun.ErrAttributeNotFound:
			lifetime.Duration = time.Minute // default
		default:
			return errors.Wrap(err, "failed to get lifetime")
		}
		s.log.Info("processing create permission request")
		if err := s.allocs.CreatePermission(client, allocator.Addr(addr), now.Add(lifetime.Duration)); err != nil {
			return errors.Wrap(err, "failed to create allocation")
		}
		return res.Build(req,
			stun.NewType(stun.MethodCreatePermission, stun.ClassSuccessResponse),
		)
	case turn.RefreshRequest:
		var (
			addr     turn.PeerAddress
			lifetime turn.Lifetime
		)
		if err := req.Parse(&addr); err != nil && err != stun.ErrAttributeNotFound {
			return errors.Wrap(err, "failed to parse refresh request")
		}
		if err := req.Parse(&addr); err != nil {
			if err != stun.ErrAttributeNotFound {
				return errors.Wrap(err, "failed to parse")
			}
		}
		switch lifetime.Duration {
		case 0:
			s.allocs.Remove(client)
		default:
			t := now.Add(lifetime.Duration)
			if err := s.allocs.Refresh(client, allocator.Addr(addr), t); err != nil {
				s.log.Error("failed to refresh allocation", zap.Error(err))
				return res.Build(req, stun.NewType(stun.MethodRefresh, stun.ClassSuccessResponse),
					stun.CodeServerError,
				)
			}
		}
		return res.Build(req,
			stun.NewType(stun.MethodRefresh, stun.ClassSuccessResponse),
		)
	case turn.SendIndication:
		var (
			data turn.Data
			addr turn.PeerAddress
		)
		if err := req.Parse(&data, &addr); err != nil {
			return errors.Wrap(err, "failed to parse send indication")
		}
		if err := s.sendByPermission(data, client, addr); err != nil {
			s.log.Warn("send failed",
				zap.Error(err),
			)
		}
		return nil
	default:
		return errors.Errorf("unknown request type %s", req.Type)
	}
}

func (s *Server) serveConn(c net.PacketConn, res, req *stun.Message) error {
	if c == nil {
		return nil
	}
	buf := make([]byte, 1024)
	n, addr, err := c.ReadFrom(buf)
	if err != nil {
		s.log.Warn("readFrom failed", zap.Error(err))
		return nil
	}
	s.log.Debug("read",
		zap.Int("n", n),
		zap.Stringer("addr", addr),
	)
	if _, err = req.Write(buf[:n]); err != nil {
		s.log.Warn("write failed", zap.Error(err))
		return err
	}
	if err = s.process(addr, buf[:n], req, res); err != nil {
		if err == errNotSTUNMessage {
			return nil
		}
		s.log.Error("process failed", zap.Error(err))
		return nil
	}
	if len(res.Raw) == 0 {
		// Indication.
		return nil
	}
	_, err = c.WriteTo(res.Raw, addr)
	if err != nil {
		s.log.Warn("writeTo failed", zap.Error(err))
	}
	return err
}

// Serve reads packets from connections and responds to BINDING requests.
func (s *Server) Serve() error {
	var (
		res = new(stun.Message)
		req = new(stun.Message)
	)
	for {
		if err := s.serveConn(s.conn, res, req); err != nil {
			s.log.Error("serveConn failed", zap.Error(err))
		}
		res.Reset()
		req.Reset()
	}
}
