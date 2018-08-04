package allocator

import (
	"net"

	"github.com/gortc/turn"
)

// SystemPortAllocator allocates port directly on system.
type SystemPortAllocator struct{}

// AllocatePort returns new requested initialized NetAllocation.
func (s SystemPortAllocator) AllocatePort(
	proto turn.Protocol, network, defaultAddr string,
) (NetAllocation, error) {
	addr, err := net.ResolveUDPAddr(network, defaultAddr)
	if err != nil {
		return NetAllocation{}, err
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return NetAllocation{}, err
	}
	realAddr := conn.LocalAddr().(*net.UDPAddr)
	a := NetAllocation{
		Proto: proto,
		Addr: Addr{
			Port: realAddr.Port,
			IP:   realAddr.IP,
		},
		Conn: conn,
	}
	return a, nil
}
