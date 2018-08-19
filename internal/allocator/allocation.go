package allocator

import (
	"fmt"
	"io"
	"net"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/gortc/turn"
)

// PeerHandler represents handler for data that is sent to relayed address
// of allocation.
type PeerHandler interface {
	HandlePeerData(d []byte, t turn.FiveTuple, a turn.Addr)
}

// Permission as described in "Permissions" section, mimics the
// address-restricted filtering mechanism of NAT's.
//
// See RFC 5766 Section 2.3
type Permission struct {
	Addr    turn.Addr
	Timeout time.Time
	Binding turn.ChannelNumber // 0 or valid channel number
}

func (p Permission) String() string {
	if p.Binding == 0 {
		return fmt.Sprintf("%s [%s]", p.Addr, p.Timeout.Format(time.RFC3339))
	}
	return fmt.Sprintf("%s (c%s) [%s]", p.Addr, p.Binding, p.Timeout.Format(time.RFC3339))
}

func (p *Permission) conflicts(n turn.ChannelNumber, peer turn.Addr) bool {
	if p.Addr.Equal(peer) && (p.Binding == n || p.Binding == 0) {
		return false
	}
	return !p.Addr.Equal(peer) || p.Binding == n
}

// Allocation as described in "Allocations" section.
//
// See RFC 5766 Section 2.2
type Allocation struct {
	Tuple       turn.FiveTuple
	Permissions []Permission
	RelayedAddr turn.Addr      // relayed transport address
	Conn        net.PacketConn // on RelayedAddr
	Callback    PeerHandler    // for data from Conn
	Timeout     time.Time      // time-to-expiry
	Buf         []byte         // read buffer
	Log         *zap.Logger
}

// ReadUntilClosed starts network loop that passes all received data to
// PeerHandler. Stops on connection close or any error.
func (a *Allocation) ReadUntilClosed() {
	a.Log.Debug("start")
	defer func() {
		a.Log.Debug("stop")
	}()
	for {
		if err := a.Conn.SetReadDeadline(time.Now().Add(time.Minute)); err != nil {
			a.Log.Warn("SetReadDeadline failed", zap.Error(err))
			break
		}
		n, addr, err := a.Conn.ReadFrom(a.Buf)
		if err != nil && err != io.EOF {
			netErr, ok := err.(net.Error)
			if ok && (netErr.Temporary() || netErr.Timeout()) {
				continue
			}
			a.Log.Error("read",
				zap.Error(err),
			)
			break
		}
		if ce := a.Log.Check(zapcore.DebugLevel, "read"); ce != nil {
			ce.Write(zap.Int("n", n))
		}
		udpAddr := addr.(*net.UDPAddr)
		a.Callback.HandlePeerData(a.Buf[:n], a.Tuple, turn.Addr{
			IP:   udpAddr.IP,
			Port: udpAddr.Port,
		})
	}
}
