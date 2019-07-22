package allocator

import (
	"crypto/rand"
	"errors"
	"io"
	"math/big"
	mathRand "math/rand"
	"net"
	"sync"

	"go.uber.org/zap"

	"gortc.io/turn"
)

type pooledPort struct {
	port      int
	addr      *net.UDPAddr
	conn      *net.UDPConn
	allocated bool
}

// SystemPortPooledAllocator pre-allocates pool of ports.
type SystemPortPooledAllocator struct {
	log     *zap.Logger
	network string
	ip      net.IP
	minPort int
	maxPort int
	ports   []pooledPort
	free    []int
	mux     sync.RWMutex
	rand    io.Reader
}

// Close de-allocates all ports.
func (a *SystemPortPooledAllocator) Close() error {
	a.mux.Lock()
	for i := range a.ports {
		if err := a.ports[i].conn.Close(); err != nil {
			a.log.Warn("failed to close conn while shutdown", zap.Error(err))
		}
	}
	a.ports = a.ports[:0]
	a.mux.Unlock()
	return nil
}

type wrappedConn struct {
	net.PacketConn
	allocator *SystemPortPooledAllocator
	port      int
}

func (w *wrappedConn) Close() error {
	w.allocator.dealloc(w.port)
	return nil
}

func (a *SystemPortPooledAllocator) randomFree() pooledPort {
	// Assuming a.mux is locked.
	max := big.NewInt(int64(len(a.free)))
	i := 0
	// Trying to get cryptographically random port.
	n, err := rand.Int(a.rand, max)
	if err == nil {
		i = int(n.Int64())
	} else {
		// Falling back to pseudo-random.
		i = mathRand.Intn(len(a.free))
	}
	return a.ports[i]
}

func (a *SystemPortPooledAllocator) allocate() (NetAllocation, error) {
	a.mux.Lock()
	var p pooledPort
	a.free = a.free[:0]
	for i := range a.ports {
		if a.ports[i].allocated {
			continue
		}
		a.free = append(a.free, i)
	}
	p = a.randomFree()
	a.mux.Unlock()
	if p.conn == nil {
		return NetAllocation{}, errors.New("out of capacity")
	}
	return NetAllocation{
		Addr: turn.Addr{
			Port: p.port,
			IP:   a.ip,
		},
		Proto: turn.ProtoUDP,
		Conn: &wrappedConn{
			allocator:  a,
			PacketConn: p.conn,
		},
	}, nil
}

func (a *SystemPortPooledAllocator) dealloc(port int) {
	a.mux.Lock()
	for i := range a.ports {
		if a.ports[i].port != port {
			continue
		}
		port := a.ports[i]
		if err := port.conn.Close(); err != nil {
			a.log.Warn("failed to close on dealloc", zap.Error(err))
		}
		newConn, err := net.ListenUDP(a.network, port.addr)
		if err != nil {
			a.log.Warn("failed to listen on dealloc", zap.Error(err))
			break
		}
		a.ports[i].allocated = false
		a.ports[i].conn = newConn
		break
	}
	a.mux.Unlock()
}

func (a *SystemPortPooledAllocator) init() error {
	if a.minPort > a.maxPort {
		return errors.New("minPort is larger that maxPort")
	}
	a.mux.Lock()
	for port := a.minPort; port <= a.maxPort; port++ {
		addr := &net.UDPAddr{
			IP:   a.ip,
			Port: port,
		}
		conn, err := net.ListenUDP(a.network, addr)
		if err != nil {
			a.log.Error("failed to pre-allocate", zap.Error(err))
			return err
		}
		a.ports = append(a.ports, pooledPort{
			port: port,
			addr: addr,
			conn: conn,
		})
	}
	ports := len(a.ports)
	a.log.Info("pre-allocated", zap.Int("pool", ports))
	a.mux.Unlock()
	if ports == 0 {
		return errors.New("failed to initialize pool")
	}
	return nil
}
