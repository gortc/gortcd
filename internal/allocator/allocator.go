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
	for _, alloc := range a.allocs {
		if !alloc.Tuple.Client.Equal(client) {
			continue
		}
		for _, p := range alloc.Permissions {
			if !addr.Equal(p.Addr) {
				continue
			}
			conn = alloc.Conn
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

// Remove de-allocates any permissions for client and removes allocation.
func (a *Allocator) Remove(client Addr) {
	var (
		newAllocs []Allocation
		toDealloc []Allocation
	)

	a.allocsMux.Lock()
	for _, a := range a.allocs {
		if !a.Tuple.Client.Equal(client) {
			newAllocs = append(newAllocs, a)
			continue
		}
		toDealloc = append(toDealloc, a)
	}
	n := copy(a.allocs, newAllocs)
	a.allocs = a.allocs[:n]
	a.allocsMux.Unlock()

	for _, alloc := range toDealloc {
		a.raddr.Remove(alloc.Tuple.Server, alloc.Tuple.Proto)
	}
}

// Collect removes any timed out permissions. If allocation has no
// active permissions, it will be removed.
func (a *Allocator) Collect(t time.Time) {
	var (
		newAllocs []Allocation
		toDealloc []Allocation
	)

	a.allocsMux.Lock()
	for _, a := range a.allocs {
		var newPermissions []Permission
		for _, p := range a.Permissions {
			if p.Timeout.After(t) {
				newPermissions = append(newPermissions, p)
				continue
			}
		}
		n := copy(a.Permissions, newPermissions)
		a.Permissions = a.Permissions[:n]
		if n > 0 {
			newAllocs = append(newAllocs, a)
		} else {
			toDealloc = append(toDealloc, a)
		}
	}
	n := copy(a.allocs, newAllocs)
	a.allocs = a.allocs[:n]
	a.allocsMux.Unlock()

	for _, p := range toDealloc {
		a.Remove(p.Tuple.Client)
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
	a.log.Info("processing allocate request")

	a.allocsMux.Lock()
	// Searching for existing allocation.
	for _, alloc := range a.allocs {
		if alloc.Tuple.Equal(tuple) {
			a.allocsMux.Unlock()
			return Addr{}, ErrAllocationMismatch
		}
	}
	// Not found, creating new allocation.
	allocation := Allocation{
		Log:      a.log,
		Tuple:    tuple,
		Callback: callback,
		Timeout:  timeout,
	}
	a.allocs = append(a.allocs, allocation)
	a.allocsMux.Unlock()

	// Allocating new relayed address.
	addr, conn, err := a.raddr.New(tuple.Proto)
	if err != nil {
		return addr, errors.Wrap(err, "failed to allocate")
	}
	a.log.Info("allocated", zap.Stringer("addr", addr))

	a.allocsMux.Lock()
	for i := range a.allocs {
		if !a.allocs[i].Tuple.Equal(tuple) {
			continue
		}
		// Setting relayed connection for allocation.
		allocation.Conn = conn
		allocation.RelayedAddr = addr
		a.allocs[i] = allocation
	}
	a.allocsMux.Unlock()

	go allocation.ReadUntilClosed()
	return addr, nil
}

// CreatePermission creates new permission for existing client allocation.
func (a *Allocator) CreatePermission(client, addr Addr, timeout time.Time) error {
	permission := Permission{
		Timeout: timeout,
		Addr:    addr,
	}
	a.allocsMux.Lock()
	defer a.allocsMux.Unlock()
	for i, alloc := range a.allocs {
		if !alloc.Tuple.Client.Equal(client) {
			continue
		}
		switch alloc.Tuple.Proto {
		case turn.ProtoUDP:
			a.log.Info("created udp permission", zap.Stringer("t", alloc.Tuple))
		default:
			return errors.Errorf("proto %s not implemented", alloc.Tuple.Proto)
		}
		alloc.Permissions = append(alloc.Permissions, permission)
		a.allocs[i] = alloc
	}
	return nil
}

// Refresh updates existing permission timeout to t.
func (a *Allocator) Refresh(client, addr Addr, t time.Time) error {
	a.allocsMux.Lock()
	for _, a := range a.allocs {
		if !a.Tuple.Client.Equal(client) {
			continue
		}
		for i := range a.Permissions {
			p := a.Permissions[i]
			if !addr.Equal(p.Addr) {
				continue
			}
			p.Timeout = t
			a.Permissions[i] = p
		}
	}
	a.allocsMux.Unlock()
	return nil
}
