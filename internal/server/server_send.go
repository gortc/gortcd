package server

import (
	"go.uber.org/zap"

	"github.com/gortc/turn"
)

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
	addr turn.Addr,
	data []byte,
) error {
	s.log.Debug("searching for allocation",
		zap.Stringer("tuple", ctx.tuple),
		zap.Stringer("addr", addr),
	)
	_, err := s.allocs.Send(ctx.tuple, addr, data)
	return err
}
