package allocator

import (
	"net"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/gortc/turn"
)

func TestAllocator_New(t *testing.T) {
	d := &DummyNetPortAlloc{
		currentPort: 5100,
	}
	now := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	allocateIP := net.IPv4(127, 1, 0, 2)
	p, err := NewNetAllocator(zap.NewNop(), &net.UDPAddr{
		IP:   allocateIP,
		Port: 5000,
	}, d)
	if err != nil {
		t.Fatal(err)
	}
	a := NewAllocator(zap.NewNop(), p)
	client := Addr{
		Port: 200,
		IP:   net.IPv4(127, 0, 0, 1),
	}
	server := Addr{
		Port: 300,
		IP:   net.IPv4(127, 0, 0, 1),
	}
	peer := Addr{
		Port: 201,
		IP:   net.IPv4(127, 0, 0, 1),
	}
	timeout := now.Add(time.Second * 10)
	tuple := FiveTuple{
		Client: client,
		Server: server,
		Proto:  turn.ProtoUDP,
	}
	relayedAddr, err := a.New(tuple, timeout, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectedAddr := Addr{
		Port: 5101,
		IP:   allocateIP,
	}
	if !expectedAddr.Equal(relayedAddr) {
		t.Errorf("unexpected relayed addr: %s", relayedAddr)
	}
	if err := a.CreatePermission(client, peer, now.Add(time.Second*10)); err != nil {
		t.Error(err)
	}
	a.Collect(now)
	if err := a.Refresh(client, peer, now.Add(time.Second*15)); err != nil {
		t.Error(err)
	}
	a.Collect(now.Add(time.Second * 11))
	if _, err := a.Send(client, peer, make([]byte, 100)); err != nil {
		t.Error(err)
	}
	a.Collect(now.Add(time.Second * 17))
	if _, err := a.Send(client, peer, make([]byte, 100)); err != ErrPermissionNotFound {
		t.Errorf("unexpected err: %v", err)
	}
	relayedAddr, err = a.New(tuple, timeout, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectedAddr = Addr{
		Port: 5102,
		IP:   allocateIP,
	}
	if !expectedAddr.Equal(relayedAddr) {
		t.Errorf("unexpected relayed addr: %s", relayedAddr)
	}
	a.Remove(client)
}
