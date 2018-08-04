package server

import (
	"time"

	"github.com/gortc/gortcd/internal/allocator"
	"github.com/gortc/stun"
	"github.com/gortc/turn"
)

type context struct {
	time      time.Time
	client    allocator.Addr
	server    allocator.Addr
	request   *stun.Message
	response  *stun.Message
	cdata     *turn.ChannelData
	nonce     stun.Nonce
	realm     stun.Realm
	integrity stun.MessageIntegrity
	software  stun.Software
	buf       []byte // buf request
}

func (c *context) reset() {
	c.time = time.Time{}
	c.client = allocator.Addr{}
	c.server = allocator.Addr{}
	c.request.Reset()
	c.response.Reset()
	c.cdata.Reset()
	c.nonce = c.nonce[:0]
	c.realm = c.realm[:0]
	c.integrity = nil
	c.software = c.software[:0]
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
