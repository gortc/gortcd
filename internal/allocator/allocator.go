// Package allocator implements turn allocation management.
//
// Will be eventually stabilized and moved to gortc/turn package.
package allocator

import (
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/gortc/turn"
)

// NewAllocator initializes and returns new *Allocator.
func NewAllocator(log *zap.Logger, conn RelayedAddrAllocator) *Allocator {
	return &Allocator{
		log:   log,
		raddr: conn,
	}
}

// Allocator handles allocation.
type Allocator struct {
	log       *zap.Logger
	allocsMux sync.RWMutex
	allocs    []Allocation
	raddr     RelayedAddrAllocator
}

// ErrPermissionNotFound means that requested allocation (client,addr) is not found.
var ErrPermissionNotFound = errors.New("permission not found")

// Send uses existing allocation for client to write data to remote addr.
//
// Returns ErrPermissionNotFound if no allocation found for (client,addr).
func (a *Allocator) Send(client, addr Addr, data []byte) (int, error) {
	var (
		conn net.PacketConn
	)
	a.log.Info("searching for allocation",
		zap.Stringer("client", client),
		zap.Stringer("addr", addr),
	)
	a.allocsMux.RLock()
	for i := range a.allocs {
		if !a.allocs[i].Tuple.Client.Equal(client) {
			continue
		}
		for _, p := range a.allocs[i].Permissions {
			if !addr.Equal(p.Addr) {
				continue
			}
			conn = a.allocs[i].Conn
		}
	}
	a.allocsMux.RUnlock()
	if conn == nil {
		return 0, ErrPermissionNotFound
	}
	a.log.Debug("sending data",
		zap.Stringer("client", client),
		zap.Stringer("addr", addr),
		zap.Int("len", len(data)),
	)
	return conn.WriteTo(data, &net.UDPAddr{
		IP:   addr.IP,
		Port: addr.Port,
	})
}

// Remove de-allocates and removes allocation.
func (a *Allocator) Remove(t FiveTuple) {
	var (
		newAllocs []Allocation
		toDealloc []Allocation
	)

	a.allocsMux.Lock()
	for i := range a.allocs {
		if !a.allocs[i].Tuple.Equal(t) {
			newAllocs = append(newAllocs, a.allocs[i])
			continue
		}
		toDealloc = append(toDealloc, a.allocs[i])
	}
	n := copy(a.allocs, newAllocs)
	a.allocs = a.allocs[:n]
	a.allocsMux.Unlock()

	for i := range toDealloc {
		if err := a.raddr.Remove(toDealloc[i].Tuple.Server, toDealloc[i].Tuple.Proto); err != nil {
			a.log.Warn("failed to remove allocation", zap.Error(err))
		}
	}
}

// Collect removes any timed out permissions or allocations.
func (a *Allocator) Collect(t time.Time) {
	var (
		newAllocs []Allocation
		toDealloc []Allocation
	)

	a.allocsMux.Lock()
	for i := range a.allocs {
		var newPermissions []Permission
		for _, p := range a.allocs[i].Permissions {
			if p.Timeout.After(t) {
				newPermissions = append(newPermissions, p)
				continue
			}
		}
		n := copy(a.allocs[i].Permissions, newPermissions)
		a.allocs[i].Permissions = a.allocs[i].Permissions[:n]

		if a.allocs[i].Timeout.After(t) {
			newAllocs = append(newAllocs, a.allocs[i])
		} else {
			toDealloc = append(toDealloc, a.allocs[i])
		}
	}
	n := copy(a.allocs, newAllocs)
	a.allocs = a.allocs[:n]
	a.allocsMux.Unlock()

	for i := range toDealloc {
		if err := a.raddr.Remove(toDealloc[i].Tuple.Server, toDealloc[i].Tuple.Proto); err != nil {
			a.log.Warn("failed to remove allocation", zap.Error(err))
		}
	}
}

// RelayedAddrAllocator represents allocator for relayed addresses on
// specified interface.
type RelayedAddrAllocator interface {
	New(proto turn.Protocol) (Addr, net.PacketConn, error)
	Remove(addr Addr, proto turn.Protocol) error
}

// ErrAllocationMismatch is a 437 (Allocation Mismatch) error
var ErrAllocationMismatch = errors.New("5-tuple is currently in use")

// New creates new allocation for provided client and proto. Any data received
// by allocated socket is passed to callback.
func (a *Allocator) New(tuple FiveTuple, timeout time.Time, callback PeerHandler) (Addr, error) {
	l := a.log.Named("allocation").With(zap.Stringer("tuple", tuple))
	l.Debug("new", zap.Time("timeout", timeout))

	a.allocsMux.Lock()
	// Searching for existing allocation.
	for i := range a.allocs {
		if a.allocs[i].Tuple.Equal(tuple) {
			a.allocsMux.Unlock()
			// The 5-tuple is currently in use by an existing allocation,
			// returning allocation mismatch error.
			return Addr{}, ErrAllocationMismatch
		}
	}
	// Not found, creating new allocation.
	allocation := Allocation{
		Log:      l,
		Tuple:    tuple,
		Callback: callback,
		Timeout:  timeout,
	}
	a.allocs = append(a.allocs, allocation)
	a.allocsMux.Unlock()

	raddr, conn, err := a.raddr.New(tuple.Proto)
	if err != nil {
		a.log.Error("failed",
			zap.Stringer("tuple", tuple),
			zap.Error(err),
		)
		return Addr{}, errors.Wrap(err, "failed to allocate")
	}
	l = l.With(zap.Stringer("raddr", raddr))
	l.Info("ok")
	buf := make([]byte, 2048)

	a.allocsMux.Lock()
	for i := range a.allocs {
		if !a.allocs[i].Tuple.Equal(tuple) {
			continue
		}
		allocation.Conn = conn
		allocation.RelayedAddr = raddr
		allocation.Buf = buf
		allocation.Log = l
		a.allocs[i] = allocation
		break
	}
	a.allocsMux.Unlock()

	go allocation.ReadUntilClosed()
	return raddr, nil
}

// CreatePermission creates new permission for existing client allocation.
func (a *Allocator) CreatePermission(tuple FiveTuple, peer Addr, timeout time.Time) error {
	permission := Permission{
		Timeout: timeout,
		Addr:    peer,
	}
	switch tuple.Proto {
	case turn.ProtoUDP:
		// pass
	default:
		return errors.Errorf("proto %s not implemented", tuple.Proto)
	}
	var found bool
	a.allocsMux.Lock()
	for i := range a.allocs {
		if !a.allocs[i].Tuple.Equal(tuple) {
			continue
		}
		a.allocs[i].Permissions = append(a.allocs[i].Permissions, permission)
		found = true
		break
	}
	a.allocsMux.Unlock()
	if !found {
		return ErrAllocationMismatch
	}
	a.log.Debug("created permission",
		zap.Stringer("tuple", tuple),
		zap.Stringer("peer", peer),
		zap.Time("timeout", timeout),
	)
	return nil
}

// Refresh updates existing permission timeout to t.
func (a *Allocator) Refresh(tuple FiveTuple, peerAddr Addr, timeout time.Time) error {
	// TODO: handle permission not found error.
	a.allocsMux.Lock()
	for i := range a.allocs {
		if !a.allocs[i].Tuple.Equal(tuple) {
			continue
		}
		for k := range a.allocs[i].Permissions {
			p := a.allocs[i].Permissions[k]
			if !peerAddr.Equal(p.Addr) {
				continue
			}
			p.Timeout = timeout
			a.allocs[i].Permissions[k] = p
			break
		}
		break
	}
	a.allocsMux.Unlock()
	return nil
}
