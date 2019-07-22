package allocator

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"gortc.io/turn"
)

// DummyNetPortAlloc is dummy allocator for testing purposes.
type DummyNetPortAlloc struct {
	currentPort int32
}

type dummyConn struct {
	closed    bool
	closedMux sync.Mutex
}

type dummyErrNetPortAlloc struct {
	err error
}

func (d dummyErrNetPortAlloc) AllocatePort(proto turn.Protocol, network, defaultAddr string) (NetAllocation, error) {
	return NetAllocation{}, d.err
}

var (
	errDummyConnReadFrom = errors.New("readFrom")
	errDummyConnClosed   = errors.New("closed")
)

func (c *dummyConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	c.closedMux.Lock()
	defer c.closedMux.Unlock()
	if c.closed {
		return 0, nil, errDummyConnClosed
	}
	// TODO: improve
	return 0, nil, errDummyConnReadFrom
}

func (c *dummyConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	c.closedMux.Lock()
	defer c.closedMux.Unlock()
	if c.closed {
		return 0, errDummyConnClosed
	}
	return len(p), nil
}

func (c *dummyConn) Close() error {
	c.closedMux.Lock()
	defer c.closedMux.Unlock()
	if c.closed {
		return errDummyConnClosed
	}
	c.closed = true
	return nil
}

func (*dummyConn) LocalAddr() net.Addr {
	return &net.UDPAddr{}
}

func (*dummyConn) SetDeadline(t time.Time) error {
	panic("implement me")
}

func (*dummyConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (*dummyConn) SetWriteDeadline(t time.Time) error {
	panic("implement me")
}

func (p *DummyNetPortAlloc) AllocatePort(
	proto turn.Protocol, network, defaultAddr string,
) (NetAllocation, error) {
	h, _, _ := net.SplitHostPort(defaultAddr)
	ip := net.ParseIP(h)
	return NetAllocation{
		Proto: proto,
		Addr: turn.Addr{
			Port: int(atomic.AddInt32(&p.currentPort, 1)),
			IP:   ip,
		},
		Conn: &dummyConn{},
	}, nil
}

func TestNetAllocation(t *testing.T) {
	d := &DummyNetPortAlloc{}
	t.Run("NonUDP", func(t *testing.T) {
		_, err := NewNetAllocator(zap.NewNop(), &net.TCPAddr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: 5000,
		}, d)
		if err == nil {
			t.Error("Should error")
		}
	})
	p, err := NewNetAllocator(zap.NewNop(), &net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 5000,
	}, d)
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := p.New(turn.ProtoUDP)
	if err != nil {
		t.Fatal(err)
	}
	if a.IP == nil {
		t.Error("a.IP is nil")
	}
	a2, c2, err := p.New(turn.ProtoUDP)
	if err != nil {
		t.Fatal(err)
	}
	c2.Close()
	a3, _, err := p.New(2)
	if err != nil {
		t.Fatal(err)
	}
	p.Remove(a, turn.ProtoUDP)
	p.Remove(a2, turn.ProtoUDP)
	p.Remove(a2, turn.ProtoUDP)
	p.Remove(a3, turn.ProtoUDP)
}
