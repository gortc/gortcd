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

func NewAllocator(log *zap.Logger, conn PortAllocator) *Allocator {
	return &Allocator{
		log:  log,
		conn: conn,
	}
}

type Allocator struct {
	log       *zap.Logger
	allocsMux sync.RWMutex
	allocs    []Allocation
	conn      PortAllocator
}

var ErrPermissionNotFound = errors.New("permission not found")

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
		a.conn.Remove(alloc.Tuple.Server, alloc.Tuple.Proto)
	}
}

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

type PortAllocator interface {
	New(proto turn.Protocol) (Addr, net.PacketConn, error)
	Remove(addr Addr, proto turn.Protocol) error
}

func (a *Allocator) New(client Addr, proto turn.Protocol, callback PeerHandler) (Addr, error) {
	a.log.Info("processing allocate request")
	addr, conn, err := a.conn.New(proto)
	if err != nil {
		return addr, errors.Wrap(err, "failed to allocate")
	}
	a.log.Info("allocated", zap.Stringer("addr", addr))
	a.allocsMux.Lock()
	tuple := FiveTuple{
		Client: client,
		Server: addr,
		Proto:  proto,
	}
	allocation := Allocation{
		Log:      a.log,
		Tuple:    tuple,
		Callback: callback,
		Conn:     conn,
	}
	a.allocs = append(a.allocs, allocation)
	a.allocsMux.Unlock()
	go allocation.ReadUntilClosed()
	return addr, nil
}

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
