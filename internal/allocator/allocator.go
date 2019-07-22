// Package allocator implements turn allocation management.
//
// Will be eventually stabilized and moved to gortc/turn package.
package allocator

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"gortc.io/turn"
)

// Options contain possible settings for Allocator.
type Options struct {
	Log    *zap.Logger
	Conn   RelayedAddrAllocator
	Labels prometheus.Labels
}

// NewAllocator initializes and returns new *Allocator.
func NewAllocator(o Options) *Allocator {
	if o.Log == nil {
		o.Log = zap.NewNop()
	}
	return &Allocator{
		log:   o.Log,
		raddr: o.Conn,
		metrics: map[string]*prometheus.Desc{
			"allocation_count": prometheus.NewDesc("gortcd_allocation_count",
				"Total number of allocations.", []string{}, o.Labels),
			"permission_count": prometheus.NewDesc("gortcd_permission_count",
				"Total number of permissions.", []string{}, o.Labels),
			"binding_count": prometheus.NewDesc("gortcd_binding_count",
				"Total number of bindings.", []string{}, o.Labels),
		},
	}
}

// Allocator handles allocation.
type Allocator struct {
	log       *zap.Logger
	allocsMux sync.RWMutex
	allocs    []Allocation
	raddr     RelayedAddrAllocator
	metrics   map[string]*prometheus.Desc
}

// Describe implements Collector.
func (a *Allocator) Describe(c chan<- *prometheus.Desc) {
	for _, d := range a.metrics {
		c <- d
	}
}

// Collect implements Collector.
func (a *Allocator) Collect(c chan<- prometheus.Metric) {
	s := a.Stats()
	for _, m := range []prometheus.Metric{
		prometheus.MustNewConstMetric(
			a.metrics["allocation_count"],
			prometheus.GaugeValue,
			float64(s.Allocations),
		),
		prometheus.MustNewConstMetric(
			a.metrics["permission_count"],
			prometheus.GaugeValue,
			float64(s.Permissions),
		),
		prometheus.MustNewConstMetric(
			a.metrics["binding_count"],
			prometheus.GaugeValue,
			float64(s.Bindings),
		),
	} {
		c <- m
	}
}

// ErrPermissionNotFound means that requested allocation (client,addr) is not found.
var ErrPermissionNotFound = errors.New("permission not found")

// SendBound uses existing allocation identified by tuple with bound channel number n
// to send data.
func (a *Allocator) SendBound(tuple turn.FiveTuple, n turn.ChannelNumber, data []byte) (int, error) {
	var (
		conn net.PacketConn
		addr turn.Addr
	)
	if ce := a.log.Check(zapcore.DebugLevel, "searching for bound allocation"); ce != nil {
		ce.Write(zap.Stringer("tuple", tuple), zap.Stringer("n", n))
	}
	a.allocsMux.RLock()
	for i := range a.allocs {
		if !a.allocs[i].Tuple.Equal(tuple) {
			continue
		}
		for _, p := range a.allocs[i].Permissions {
			if len(p.Bindings) == 0 {
				continue
			}
			for _, b := range p.Bindings {
				if b.Channel != n {
					continue
				}
				conn = a.allocs[i].Conn
				// Copy p.Addr to turn.Addr.
				addr = turn.Addr{
					Port: b.Port,
					IP:   make(net.IP, len(p.IP)),
				}
				copy(addr.IP, p.IP)
			}
		}
	}
	a.allocsMux.RUnlock()
	if conn == nil {
		return 0, ErrPermissionNotFound
	}
	a.log.Debug("sending data",
		zap.Stringer("tuple", tuple),
		zap.Stringer("addr", addr),
		zap.Int("len", len(data)),
		zap.Stringer("laddr", conn.LocalAddr()),
		zap.Stringer("raddr", &net.UDPAddr{
			IP:   addr.IP,
			Port: addr.Port,
		}),
	)
	return conn.WriteTo(data, &net.UDPAddr{
		IP:   addr.IP,
		Port: addr.Port,
	})
}

// Send uses existing allocation for client to write data to remote turn.Addr.
//
// Returns ErrPermissionNotFound if no allocation found for (client,addr).
func (a *Allocator) Send(tuple turn.FiveTuple, peer turn.Addr, data []byte) (int, error) {
	var (
		conn net.PacketConn
	)
	a.log.Debug("searching for allocation",
		zap.Stringer("t", tuple),
		zap.Stringer("peer", peer),
	)
	a.allocsMux.RLock()
	for i := range a.allocs {
		if !a.allocs[i].Tuple.Equal(tuple) {
			continue
		}
		for _, p := range a.allocs[i].Permissions {
			if !peer.IP.Equal(p.IP) {
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
		zap.Stringer("tuple", tuple),
		zap.Stringer("addr", peer),
		zap.Int("len", len(data)),
	)
	return conn.WriteTo(data, &net.UDPAddr{
		IP:   peer.IP,
		Port: peer.Port,
	})
}

// Remove de-allocates and removes allocation.
func (a *Allocator) Remove(t turn.FiveTuple) error {
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
	if len(toDealloc) == 0 {
		return ErrAllocationMismatch
	}
	for i := range toDealloc {
		if err := a.raddr.Remove(toDealloc[i].Tuple.Server, toDealloc[i].Tuple.Proto); err != nil {
			a.log.Warn("failed to remove allocation", zap.Error(err))
		}
	}
	return nil
}

// Prune removes any timed out permissions or allocations.
func (a *Allocator) Prune(t time.Time) {
	var (
		newAllocs []Allocation
		toDealloc []Allocation
	)

	a.allocsMux.Lock()
	for i := range a.allocs {
		var newPermissions []Permission
		for _, p := range a.allocs[i].Permissions {
			var newBindings []Binding
			for _, b := range p.Bindings {
				if b.Timeout.After(t) {
					newBindings = append(newBindings, b)
				}
			}
			p.Bindings = newBindings
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

// RelayedAddrAllocator represents allocator for relayed turn.Addresses on
// specified interface.
type RelayedAddrAllocator interface {
	New(proto turn.Protocol) (turn.Addr, net.PacketConn, error)
	Remove(addr turn.Addr, proto turn.Protocol) error
}

// ErrAllocationMismatch is a 437 (Allocation Mismatch) error
var ErrAllocationMismatch = errors.New("5-tuple is currently in use")

// New creates new allocation for provided client and proto. Any data received
// by allocated socket is passed to callback.
func (a *Allocator) New(tuple turn.FiveTuple, timeout time.Time, callback PeerHandler) (turn.Addr, error) {
	l := a.log.Named("allocation").With(zap.Stringer("tuple", tuple))
	l.Debug("new", zap.Time("timeout", timeout))
	switch tuple.Proto {
	case turn.ProtoUDP:
		// pass
	default:
		return turn.Addr{}, errors.Errorf("proto %s not implemented", tuple.Proto)
	}
	a.allocsMux.Lock()
	// Searching for existing allocation.
	for i := range a.allocs {
		if a.allocs[i].Tuple.Equal(tuple) {
			a.allocsMux.Unlock()
			// The 5-tuple is currently in use by an existing allocation,
			// returning allocation mismatch error.
			return turn.Addr{}, ErrAllocationMismatch
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
		return turn.Addr{}, errors.Wrap(err, "failed to allocate")
	}
	l = l.With(zap.Stringer("raddr", raddr))
	l.Debug("ok")
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
func (a *Allocator) CreatePermission(tuple turn.FiveTuple, peer turn.Addr, timeout time.Time) error {
	permission := Permission{
		Timeout: timeout,
	}
	permission.IP = append(permission.IP, peer.IP...)
	var (
		found   bool
		updated bool
	)
	a.allocsMux.Lock()
	for i := range a.allocs {
		if !a.allocs[i].Tuple.Equal(tuple) {
			continue
		}
		found = true
		for k := range a.allocs[i].Permissions {
			if !a.allocs[i].Permissions[k].IP.Equal(peer.IP) {
				continue
			}
			// Updating.
			a.allocs[i].Permissions[k].Timeout = timeout
			updated = true
			break
		}
		if !updated {
			// Creating new permission instead.
			a.allocs[i].Permissions = append(a.allocs[i].Permissions, permission)
		}
		break
	}
	a.allocsMux.Unlock()
	if !found {
		return ErrAllocationMismatch
	}
	a.log.Debug("permission",
		zap.Stringer("tuple", tuple),
		zap.Stringer("peer", peer),
		zap.Bool("updated", updated),
		zap.Time("timeout", timeout),
	)
	return nil
}

// ChannelBind represents channel bind request, creating or refreshing
// channel binding.
//
// Allocator implementation does not assume any default timeout.
func (a *Allocator) ChannelBind(tuple turn.FiveTuple, n turn.ChannelNumber, peer turn.Addr, timeout time.Time) error {
	if !n.Valid() {
		return turn.ErrInvalidChannelNumber
	}
	updated := false
	found := false
	allocFound := false
	a.allocsMux.Lock()
	defer a.allocsMux.Unlock()
	for i := range a.allocs {
		if !a.allocs[i].Tuple.Equal(tuple) {
			continue
		}
		// Searching for existing permission.
		for k := range a.allocs[i].Permissions {
			pIP := a.allocs[i].Permissions[k].IP
			if !pIP.Equal(peer.IP) {
				continue
			}
			// Checking for binding conflicts.
			if a.allocs[i].Permissions[k].conflicts(n, peer) {
				// There is existing binding with same channel number or peer turn.Address.
				fmt.Printf("Conflict %+v: %d %s",
					a.allocs[i].Permissions[k],
					n, peer,
				)
				return ErrAllocationMismatch
			}
			for j := range a.allocs[i].Permissions[k].Bindings {
				if a.allocs[i].Permissions[k].Bindings[j].Channel != n {
					continue
				}
				// Updating existing binding and permission.
				a.allocs[i].Permissions[k].Bindings[j].Timeout = timeout
				if timeout.After(a.allocs[i].Permissions[k].Timeout) {
					a.allocs[i].Permissions[k].Timeout = timeout
				}
				a.log.Debug("updated binding",
					zap.Stringer("addr", peer),
					zap.Stringer("tuple", tuple),
					zap.Stringer("binding", n),
				)
				updated = true
				break
			}
			if !updated {
				// No binding found, creating new one.
				a.log.Debug("created binding",
					zap.Stringer("addr", peer),
					zap.Stringer("tuple", tuple),
					zap.Stringer("binding", n),
				)
				if timeout.After(a.allocs[i].Permissions[k].Timeout) {
					a.allocs[i].Permissions[k].Timeout = timeout
				}
				a.allocs[i].Permissions[k].Bindings = append(a.allocs[i].Permissions[k].Bindings, Binding{
					Port:    peer.Port,
					Channel: n,
					Timeout: timeout,
				})
			}
			found = true
			break
		}
		if !found {
			// No permission found, creating new one.
			a.log.Debug("created permission via binding",
				zap.Stringer("addr", peer),
				zap.Stringer("tuple", tuple),
				zap.Stringer("binding", n),
			)
			a.allocs[i].Permissions = append(a.allocs[i].Permissions, Permission{
				IP:      peer.IP,
				Timeout: timeout,
				Bindings: []Binding{
					{
						Timeout: timeout,
						Channel: n,
						Port:    peer.Port,
					},
				},
			})
		}
		allocFound = true
	}
	if !allocFound {
		// No allocation found.
		return ErrAllocationMismatch
	}
	return nil
}

// Bound returns currently bound channel for provided 5-tuple.
func (a *Allocator) Bound(tuple turn.FiveTuple, peer turn.Addr) (turn.ChannelNumber, error) {
	a.allocsMux.RLock()
	defer a.allocsMux.RUnlock()
	for i := range a.allocs {
		if !a.allocs[i].Tuple.Equal(tuple) {
			continue
		}
		for k := range a.allocs[i].Permissions {
			if !a.allocs[i].Permissions[k].IP.Equal(peer.IP) {
				continue
			}
			for j := range a.allocs[i].Permissions[k].Bindings {
				if a.allocs[i].Permissions[k].Bindings[j].Port == peer.Port {
					return a.allocs[i].Permissions[k].Bindings[j].Channel, nil
				}
			}
		}
	}
	return 0, ErrAllocationMismatch
}

// Refresh updates existing allocation timeout.
func (a *Allocator) Refresh(tuple turn.FiveTuple, timeout time.Time) error {
	// TODO: handle permission not found error.
	a.allocsMux.Lock()
	for i := range a.allocs {
		if !a.allocs[i].Tuple.Equal(tuple) {
			continue
		}
		a.allocs[i].Timeout = timeout
		break
	}
	a.allocsMux.Unlock()
	return nil
}

// Stats contains allocator statistics.
type Stats struct {
	// Allocations is the total number of allocations.
	Allocations int
	// Permissions is the total number of permissions in all allocations.
	Permissions int
	// Bindings is the total number of channel bindings in all allocations.
	Bindings int
}

// Stats returns current statistics.
func (a *Allocator) Stats() Stats {
	a.allocsMux.Lock()
	s := Stats{
		Allocations: len(a.allocs),
	}
	for i := range a.allocs {
		s.Permissions += len(a.allocs[i].Permissions)
		for k := range a.allocs[i].Permissions {
			s.Bindings += len(a.allocs[i].Permissions[k].Bindings)
		}
	}
	a.allocsMux.Unlock()
	return s
}
