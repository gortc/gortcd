package server

import (
	"net"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"gortc.io/stun"

	"gortc.io/gortcd/internal/allocator"
	"gortc.io/gortcd/internal/auth"
	"gortc.io/turn"
)

type handleFunc = func(ctx *context) error

var channelBindRequest = stun.NewType(stun.MethodChannelBind, stun.ClassRequest)

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

// HandlePeerData implements allocator.PeerHandler.
func (s *Server) HandlePeerData(d []byte, t turn.FiveTuple, a turn.Addr) {
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
	if err := m.Build(stun.TransactionID, stun.NewType(stun.MethodData, stun.ClassIndication),
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
	return ctx.buildOk((*stun.XORMappedAddress)(&ctx.client))
}

func (s *Server) processAllocateRequest(ctx *context) error {
	var transport turn.RequestedTransport
	if err := transport.GetFrom(ctx.request); err != nil {
		return ctx.buildErr(stun.CodeBadRequest)
	}
	lifetime := ctx.cfg.defaultLifetime
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
		lifetime turn.Lifetime
		allocErr error
	)
	if err := ctx.request.Parse(&lifetime); err != nil && err != stun.ErrAttributeNotFound {
		return errors.Wrap(err, "failed to parse")
	}
	switch lifetime.Duration {
	case 0:
		allocErr = s.allocs.Remove(ctx.tuple)
	default:
		timeout := ctx.time.Add(lifetime.Duration)
		allocErr = s.allocs.Refresh(ctx.tuple, timeout)
	}
	switch allocErr {
	case nil:
		return ctx.buildOk(&lifetime)
	case allocator.ErrAllocationMismatch:
		return ctx.buildErr(stun.CodeAllocMismatch)
	default:
		s.log.Error("failed to process refresh request", zap.Error(allocErr))
		return ctx.buildErr(stun.CodeServerError)
	}
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
		max := ctx.cfg.maxLifetime
		if lifetime.Duration > max {
			lifetime.Duration = max
		}
	case stun.ErrAttributeNotFound:
		lifetime.Duration = ctx.cfg.defaultLifetime
	default:
		return errors.Wrap(err, "failed to get lifetime")
	}
	s.log.Debug("processing create permission request")
	var (
		peerAddr = turn.Addr(addr)
		timeout  = ctx.time.Add(lifetime.Duration)
	)
	if !ctx.allowPeer(peerAddr) {
		// Sending 403 (Forbidden) as described in RFC 5766 Section 9.1.
		return ctx.buildErr(stun.CodeForbidden)
	}
	switch err := s.allocs.CreatePermission(ctx.tuple, peerAddr, timeout); err {
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
	if err := s.sendByPermission(ctx, turn.Addr(addr), data); err != nil {
		s.log.Warn("send failed", zap.Error(err))
	}
	return nil
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
		peerAddr = turn.Addr(addr)
		lifetime = ctx.cfg.defaultLifetime
		timeout  = ctx.time.Add(lifetime)
	)
	if !ctx.allowPeer(peerAddr) {
		// Sending 403 (Forbidden) as described in RFC 5766 Section 9.1.
		return ctx.buildErr(stun.CodeForbidden)
	}
	switch err := s.allocs.ChannelBind(ctx.tuple, number, peerAddr, timeout); err {
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
		ce.Write(zap.Int("channel", int(ctx.cdata.Number)), zap.Int("len", ctx.cdata.Length))
	}
	return s.sendByBinding(ctx, ctx.cdata.Number, ctx.cdata.Data)
}

func (s *Server) needAuth(ctx *context) bool {
	if s.auth == nil {
		return false
	}
	if ctx.request.Type.Class == stun.ClassIndication {
		return false
	}
	if ctx.request.Type == stun.BindingRequest && !ctx.cfg.authForSTUN {
		return false
	}
	return true
}

func (s *Server) processMessage(ctx *context) error {
	if err := ctx.request.Decode(); err != nil {
		if ce := s.log.Check(zapcore.DebugLevel, "failed to decode request"); ce != nil {
			ce.Write(zap.Stringer("addr", ctx.client), zap.Error(err))
		}
		return nil
	}
	ctx.realm = ctx.cfg.realm
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
			return ctx.buildErr(stun.CodeUnauthorized)
		}
		if nonceErr == auth.ErrStaleNonce {
			return ctx.buildErr(stun.CodeStaleNonce)
		}
		switch integrity, err := s.auth.Auth(ctx.request); err {
		case nil:
			ctx.integrity = integrity
		default:
			if ce := s.log.Check(zapcore.DebugLevel, "failed to auth"); ce != nil {
				ce.Write(zap.Stringer("addr", ctx.client), zap.Stringer("req", ctx.request), zap.Error(err))
			}
			return ctx.buildErr(stun.CodeUnauthorized)
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
