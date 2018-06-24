package allocator

import (
	"net"
	"testing"

	"go.uber.org/zap"

	"time"

	"github.com/gortc/turn"
)

func TestAllocator_New(t *testing.T) {
	d := &dummyNetPortAlloc{}
	now := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	p, err := NewNetAllocator(zap.NewNop(), &net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
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
	peer := Addr{
		Port: 201,
		IP:   net.IPv4(127, 0, 0, 1),
	}
	gotAddr, err := a.New(client, turn.ProtoUDP, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(gotAddr)
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
	_, err = a.New(client, turn.ProtoUDP, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.Remove(client)
}
