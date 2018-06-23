package main

import (
	"fmt"
	"net"
	"time"

	"go.uber.org/zap"

	"github.com/gortc/turn"
)

// Proto is protocol identifier.
type Proto int

const (
	UDP Proto = iota
	TCP
	DTLS
)

func (p Proto) String() string {
	switch p {
	case UDP:
		return "udp"
	case TCP:
		return "tcp"
	case DTLS:
		return "dtls"
	default:
		return fmt.Sprintf("%x", int(p))
	}
}

// Addr is ip:port.
type Addr struct {
	IP   net.IP
	Port int
}

func (a Addr) Equal(b Addr) bool {
	if a.Port != b.Port {
		return false
	}
	return a.IP.Equal(b.IP)
}

func (a Addr) String() string {
	return fmt.Sprintf("%s:%d", a.IP, a.Port)
}

// FiveTuple represents 5-TUPLE value.
type FiveTuple struct {
	Client Addr
	Server Addr
	Proto  turn.Protocol
}

func (t FiveTuple) String() string {
	return fmt.Sprintf("%s->%s (%s)",
		t.Client, t.Server, t.Proto,
	)
}

type PeerHandler interface {
	HandlePeerData(d []byte, t FiveTuple, a Addr)
}

// Permission as described in "Permissions" section.
//
// See RFC 5766 Section 2.3
type Permission struct {
	Addr     Addr
	Lifetime time.Time
	Conn     net.Conn
	Log      *zap.Logger
}

func (p Permission) Close() error {
	return p.Conn.Close()
}

// Allocation as described in "Allocations" section.
//
// See RFC 5766 Section 2.2
type Allocation struct {
	Tuple       FiveTuple
	Permissions []Permission
	Channels    []Binding
	Callback    PeerHandler
}

func (a Allocation) ReadUntilClosed(p Permission) {
	buf := make([]byte, 1024)
	for {
		p.Conn.SetReadDeadline(p.Lifetime)
		n, err := p.Conn.Read(buf)
		if err != nil {
			p.Log.Error("read", zap.Error(err))
			break
		}
		a.Callback.HandlePeerData(buf[:n], a.Tuple, p.Addr)
	}
}

// Binding is binding channel.
type Binding struct {
	Number int
	Addr   Addr
}
