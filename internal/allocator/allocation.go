package allocator

import (
	"fmt"
	"io"
	"net"
	"time"

	"go.uber.org/zap"

	"github.com/gortc/turn"
)

// Addr is ip:port.
type Addr struct {
	IP   net.IP
	Port int
}

// FromUDPAddr sets addr to UDPAddr.
func (a *Addr) FromUDPAddr(n *net.UDPAddr) {
	a.IP = n.IP
	a.Port = n.Port
}

// Equal returns true if b == a.
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

// Equal returns true if b == t.
func (t FiveTuple) Equal(b FiveTuple) bool {
	if t.Proto != b.Proto {
		return false
	}
	if !t.Client.Equal(b.Client) {
		return false
	}
	if !t.Server.Equal(b.Server) {
		return false
	}
	return true
}

// PeerHandler represents handler for data that is sent to relayed address
// of allocation.
type PeerHandler interface {
	HandlePeerData(d []byte, t FiveTuple, a Addr)
}

// Permission as described in "Permissions" section, mimics the
// address-restricted filtering mechanism of NAT's.
//
// See RFC 5766 Section 2.3
type Permission struct {
	Addr    Addr
	Timeout time.Time
}

func (p Permission) String() string {
	return fmt.Sprintf("%s [%s]", p.Addr, p.Timeout.Format(time.RFC3339))
}

// Allocation as described in "Allocations" section.
//
// See RFC 5766 Section 2.2
type Allocation struct {
	Tuple       FiveTuple
	Permissions []Permission
	Channels    []Binding
	RelayedAddr Addr           // relayed transport address
	Conn        net.PacketConn // on RelayedAddr
	Callback    PeerHandler    // for data from Conn
	Timeout     time.Time      // time-to-expiry
	Log         *zap.Logger
}

// ReadUntilClosed starts network loop that passes all received data to
// PeerHandler. Stops on connection close or any error.
func (a *Allocation) ReadUntilClosed() {
	a.Log.Debug("ReadUntilClosed")
	buf := make([]byte, 1024)
	for {
		if err := a.Conn.SetReadDeadline(time.Now().Add(time.Minute)); err != nil {
			a.Log.Warn("SetReadDeadline failed", zap.Error(err))
			break
		}
		n, addr, err := a.Conn.ReadFrom(buf)
		if err != nil && err != io.EOF {
			a.Log.Error("read", zap.Error(err))
			break
		}
		udpAddr := addr.(*net.UDPAddr)
		a.Callback.HandlePeerData(buf[:n], a.Tuple, Addr{
			IP:   udpAddr.IP,
			Port: udpAddr.Port,
		})
	}
}

// Binding is binding channel.
type Binding struct {
	Number int
	Addr   Addr
}
