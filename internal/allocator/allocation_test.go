package allocator

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"go.uber.org/zap"

	"gortc.io/turn"
)

func TestAddr_FromUDPAddr(t *testing.T) {
	u := &net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 1234,
	}
	a := new(turn.Addr)
	a.FromUDPAddr(u)
	if !u.IP.Equal(a.IP) || u.Port != a.Port {
		t.Error("not equal")
	}
}

func TestFiveTuple_Equal(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b turn.FiveTuple
		v    bool
	}{
		{
			name: "blank",
			v:    true,
		},
		{
			name: "proto",
			a: turn.FiveTuple{
				Proto: turn.ProtoUDP,
			},
		},
		{
			name: "server",
			a: turn.FiveTuple{
				Server: turn.Addr{
					Port: 100,
				},
			},
		},
		{
			name: "client",
			a: turn.FiveTuple{
				Client: turn.Addr{
					Port: 100,
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if v := tc.a.Equal(tc.b); v != tc.v {
				t.Errorf("%s [%v!=%v] %s",
					tc.a, v, tc.v, tc.b,
				)
			}
		})
	}
}

func TestFiveTuple_String(t *testing.T) {
	s := fmt.Sprint(turn.FiveTuple{
		Proto: turn.ProtoUDP,
		Server: turn.Addr{
			Port: 100,
			IP:   net.IPv4(127, 0, 0, 1),
		},
		Client: turn.Addr{
			Port: 200,
			IP:   net.IPv4(127, 0, 0, 1),
		},
	})
	if s != "127.0.0.1:200->127.0.0.1:100 (UDP)" {
		t.Error("unexpected stringer output")
	}
}

func TestPermission_String(t *testing.T) {
	p := Permission{
		IP:      net.IPv4(127, 0, 0, 1),
		Timeout: time.Date(2017, 1, 1, 1, 1, 1, 1, time.UTC),
	}
	if p.String() != "127.0.0.1 [2017-01-01T01:01:01Z]" {
		t.Error("unexpected stringer output")
	}
	p.Bindings = []Binding{
		{Port: 100, Channel: 0x4001},
	}
	if p.String() != "127.0.0.1 (b:1) [2017-01-01T01:01:01Z]" {
		t.Error("unexpected stringer output")
	}
}

type peerHandlerFunc func(d []byte, t turn.FiveTuple, a turn.Addr)

func (h peerHandlerFunc) HandlePeerData(d []byte, t turn.FiveTuple, a turn.Addr) {
	h(d, t, a)
}

type netConnMock struct {
	readFrom         func(b []byte) (n int, addr net.Addr, err error)
	writeTo          func(b []byte, addr net.Addr) (n int, err error)
	setReadDeadline  func(t time.Time) error
	setWriteDeadline func(t time.Time) error
}

func (c netConnMock) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	return c.readFrom(b)
}

func (c netConnMock) WriteTo(b []byte, addr net.Addr) (n int, err error) {
	return c.writeTo(b, addr)
}

func (netConnMock) Close() error {
	panic("implement me")
}

func (netConnMock) LocalAddr() net.Addr {
	panic("implement me")
}

func (netConnMock) SetDeadline(t time.Time) error {
	panic("implement me")
}

func (c netConnMock) SetReadDeadline(t time.Time) error {
	return c.setReadDeadline(t)
}

func (c netConnMock) SetWriteDeadline(t time.Time) error {
	return c.setWriteDeadline(t)
}

func TestAllocation_ReadUntilClosed(t *testing.T) {
	t.Run("Positive", func(t *testing.T) {
		called := false
		deadlineSet := false
		readFromCalled := false
		a := &Allocation{
			Log: zap.NewNop(),
			Conn: &netConnMock{
				setReadDeadline: func(t time.Time) error {
					deadlineSet = true
					return nil
				},
				readFrom: func(b []byte) (n int, addr net.Addr, err error) {
					if readFromCalled {
						return 0, &net.UDPAddr{}, io.ErrUnexpectedEOF
					}
					readFromCalled = true
					return 10, &net.UDPAddr{}, nil
				},
			},
			Callback: peerHandlerFunc(func(d []byte, tuple turn.FiveTuple, a turn.Addr) {
				called = true
				if len(d) != 10 {
					t.Error("incorrect length")
				}
			}),
			Buf: make([]byte, 1024),
		}
		a.ReadUntilClosed()
		if !deadlineSet {
			t.Error("deadline not set")
		}
		if !readFromCalled {
			t.Error("read from not called")
		}
		if !called {
			t.Error("callback not called")
		}
	})
	t.Run("Deadline error", func(t *testing.T) {
		deadlineSet := false
		a := &Allocation{
			Log: zap.NewNop(),
			Conn: &netConnMock{
				setReadDeadline: func(t time.Time) error {
					deadlineSet = true
					return io.ErrUnexpectedEOF
				},
			},
		}
		a.ReadUntilClosed()
		if !deadlineSet {
			t.Error("deadline not set")
		}
	})
}
