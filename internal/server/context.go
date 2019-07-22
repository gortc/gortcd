package server

import (
	"net"
	"sync"
	"time"

	"gortc.io/stun"

	"gortc.io/gortcd/internal/filter"
	"gortc.io/turn"
)

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

type context struct {
	addr      net.Addr
	conn      net.PacketConn
	cfg       config
	time      time.Time
	client    turn.Addr
	server    turn.Addr
	proto     turn.Protocol
	tuple     turn.FiveTuple
	request   *stun.Message
	response  *stun.Message
	cdata     *turn.ChannelData
	nonce     stun.Nonce
	realm     stun.Realm
	integrity stun.MessageIntegrity
	buf       []byte // buf request
}

func (c *context) allowPeer(addr turn.Addr) bool {
	return c.cfg.peerFilter.Action(addr) == filter.Allow
}

func (c *context) allowClient(addr turn.Addr) bool {
	return c.cfg.clientFilter.Action(addr) == filter.Allow
}

func (c *context) setTuple() {
	c.tuple.Proto = c.proto
	c.tuple.Client = c.client
	c.tuple.Server = c.server
}

func (c *context) reset() {
	c.addr = nil
	c.conn = nil
	c.cfg = config{}
	c.time = time.Time{}
	c.client = turn.Addr{}
	c.server = turn.Addr{}
	c.request.Reset()
	c.response.Reset()
	c.cdata.Reset()
	c.proto = 0
	c.setTuple()
	c.nonce = c.nonce[:0]
	c.realm = c.realm[:0]
	c.integrity = nil
	c.buf = c.buf[:cap(c.buf)]
	for i := range c.buf {
		c.buf[i] = 0
	}
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
	c.response.Type = stun.MessageType{
		Class:  class,
		Method: method,
	}
	c.response.TransactionID = c.request.TransactionID
	c.response.WriteHeader()
	if err := c.apply(&c.nonce, &c.realm); err != nil {
		return err
	}
	if len(c.cfg.software) > 0 {
		if err := c.cfg.software.AddTo(c.response); err != nil {
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
