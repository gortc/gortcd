package server

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"gortc.io/turn"
)

func (s *Server) sendByBinding(ctx *context, n turn.ChannelNumber, data []byte) error {
	if ce := s.log.Check(zapcore.DebugLevel, "searching for allocation via binding"); ce != nil {
		ce.Write(zap.Stringer("tuple", ctx.tuple), zap.Stringer("n", ctx.cdata.Number))
	}
	_, err := s.allocs.SendBound(ctx.tuple, n, data)
	return err
}

func (s *Server) sendByPermission(ctx *context, addr turn.Addr, data []byte) error {
	if ce := s.log.Check(zapcore.DebugLevel, "searching for allocation"); ce != nil {
		ce.Write(zap.Stringer("tuple", ctx.tuple), zap.Stringer("addr", addr))
	}
	_, err := s.allocs.Send(ctx.tuple, addr, data)
	return err
}
