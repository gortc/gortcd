// Package turn_client implements TURN client.
//
// Is subject to merge to gortc/turn when stabilized.
package turn_client

import (
	"net"
	"time"
)

type Client struct {
}

type Conn struct {
}

func (Conn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	panic("implement me")
}

func (Conn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	panic("implement me")
}

func (Conn) Close() error {
	panic("implement me")
}

func (Conn) LocalAddr() net.Addr {
	panic("implement me")
}

func (Conn) SetDeadline(t time.Time) error {
	panic("implement me")
}

func (Conn) SetReadDeadline(t time.Time) error {
	panic("implement me")
}

func (Conn) SetWriteDeadline(t time.Time) error {
	panic("implement me")
}
