package allocator

import (
	"net"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"gortc.io/turn"
)

func TestAllocator_Collect(t *testing.T) {
	d := &DummyNetPortAlloc{
		currentPort: 5100,
	}
	allocateIP := net.IPv4(127, 1, 0, 2)
	p, err := NewNetAllocator(zap.NewNop(), &net.UDPAddr{
		IP:   allocateIP,
		Port: 5000,
	}, d)
	if err != nil {
		t.Fatal(err)
	}
	a := NewAllocator(Options{Conn: p})
	c := make(chan prometheus.Metric)
	go a.Collect(c)
	expectedCount := 3
	for i := 0; i < expectedCount; i++ {
		select {
		case <-time.After(time.Millisecond * 100):
			t.Fatal("failed")
		case <-c:
			// OK
		}
	}
}

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
	a := NewAllocator(Options{Conn: p})
	client := turn.Addr{
		Port: 200,
		IP:   net.IPv4(127, 0, 0, 1),
	}
	server := turn.Addr{
		Port: 300,
		IP:   net.IPv4(127, 0, 0, 1),
	}
	peer := turn.Addr{
		Port: 201,
		IP:   net.IPv4(127, 0, 0, 1),
	}
	peer2 := turn.Addr{
		Port: 202,
		IP:   net.IPv4(127, 0, 0, 2),
	}
	timeout := now.Add(time.Second * 10)
	tuple := turn.FiveTuple{
		Client: client,
		Server: server,
		Proto:  turn.ProtoUDP,
	}
	if a.Stats().Allocations != 0 {
		t.Error("unexpected allocation count")
	}
	relayedAddr, err := a.New(tuple, timeout, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Stats().Allocations != 1 {
		t.Error("unexpected allocation count")
	}
	t.Run("AllocError", func(t *testing.T) {
		dErr := &dummyErrNetPortAlloc{
			err: net.InvalidAddrError("invalid"),
		}
		pErr, err := NewNetAllocator(zap.NewNop(), &net.UDPAddr{
			IP:   net.IPv4(127, 1, 0, 0),
			Port: 5000,
		}, dErr)
		if err != nil {
			t.Fatal(err)
		}
		aErr := NewAllocator(Options{Conn: pErr})
		if _, err := aErr.New(tuple, timeout, nil); errors.Cause(err) != dErr.err {
			t.Errorf("unexpected error: %s", err)
		}
	})
	t.Run("BadProto", func(t *testing.T) {
		if _, err := a.New(turn.FiveTuple{
			Client: client,
			Server: server,
			Proto:  1,
		}, timeout, nil); err == nil {
			t.Error("should error")
		}
	})
	expectedAddr := turn.Addr{
		Port: 5101,
		IP:   allocateIP,
	}
	if !expectedAddr.Equal(relayedAddr) {
		t.Errorf("unexpected relayed addr: %s", relayedAddr)
	}
	// Creating allocation and two permissions.
	if _, err = a.New(tuple, timeout, nil); err != ErrAllocationMismatch {
		t.Error("New() with same tuple should return mismatch error")
	}
	if a.Stats().Allocations != 1 {
		t.Error("unexpected allocation count")
	}
	if a.Stats().Permissions != 0 {
		t.Error("unexpected permissions count")
	}
	if err := a.CreatePermission(tuple, peer, now.Add(time.Second*5)); err != nil {
		t.Error(err)
	}
	if err := a.CreatePermission(tuple, peer2, now.Add(time.Second*18)); err != nil {
		t.Error(err)
	}
	if a.Stats().Permissions != 2 {
		t.Error("unexpected permissions count")
	}
	a.Prune(now)
	if a.Stats().Permissions != 2 {
		t.Error("unexpected permissions count")
	}
	// Refreshing first permission to T+8.
	if err := a.CreatePermission(tuple, peer, now.Add(time.Second*8)); err != nil {
		t.Error(err)
	}
	// Collecting at T+7.
	a.Prune(now.Add(time.Second * 7))
	// Checking that both permissions still active.
	if _, err := a.Send(tuple, peer, make([]byte, 100)); err != nil {
		t.Error(err)
	}
	if _, err := a.Send(tuple, peer2, make([]byte, 100)); err != nil {
		t.Error(err)
	}
	// Collecting T+9. First permission should expire.
	a.Prune(now.Add(time.Second * 9))
	if _, err := a.Send(tuple, peer, make([]byte, 100)); err != ErrPermissionNotFound {
		t.Errorf("unexpected err: %v", err)
	}
	if _, err := a.Send(tuple, peer2, make([]byte, 100)); err != nil {
		t.Error(err)
	}
	// Collecting T+17. Entire allocation expires.
	// Both permissions should expire too.
	a.Prune(now.Add(time.Second * 17))
	if _, err := a.Send(tuple, peer, make([]byte, 100)); err != ErrPermissionNotFound {
		t.Errorf("unexpected err: %v", err)
	}
	if _, err := a.Send(tuple, peer2, make([]byte, 100)); err != ErrPermissionNotFound {
		t.Errorf("unexpected err: %v", err)
	}
	// Attempt to create a permission with expired allocation should
	// result to allocation mismatch.
	if err := a.CreatePermission(tuple, peer, now.Add(time.Second*10)); err != ErrAllocationMismatch {
		t.Error("unexpected allocation error, should be ErrAllocationNotFound")
	}
	if a.Stats().Allocations != 0 {
		t.Errorf("unexpected allocation count")
	}
	// Re-creating allocation with same tuple should now succeed.
	relayedAddr, err = a.New(tuple, timeout, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectedAddr = turn.Addr{
		Port: 5102,
		IP:   allocateIP,
	}
	if !expectedAddr.Equal(relayedAddr) {
		t.Errorf("unexpected relayed addr: %s", relayedAddr)
	}
	if remErr := a.Remove(tuple); remErr != nil {
		t.Fatal(remErr)
	}
}

func TestAllocator_ChannelBind(t *testing.T) {
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
	a := NewAllocator(Options{Conn: p})
	client := turn.Addr{
		Port: 200,
		IP:   net.IPv4(127, 0, 0, 1),
	}
	server := turn.Addr{
		Port: 300,
		IP:   net.IPv4(127, 0, 0, 1),
	}
	peer := turn.Addr{
		Port: 201,
		IP:   net.IPv4(127, 0, 0, 1),
	}
	peer2 := turn.Addr{
		Port: 202,
		IP:   net.IPv4(127, 0, 0, 1),
	}
	const (
		n  = turn.ChannelNumber(0x4000)
		n2 = n + 1
	)
	timeout := now.Add(time.Second * 10)
	tuple := turn.FiveTuple{
		Client: client,
		Server: server,
		Proto:  turn.ProtoUDP,
	}
	relayedAddr, err := a.New(tuple, timeout, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("AllocError", func(t *testing.T) {
		dErr := &dummyErrNetPortAlloc{
			err: net.InvalidAddrError("invalid"),
		}
		pErr, err := NewNetAllocator(zap.NewNop(), &net.UDPAddr{
			IP:   net.IPv4(127, 1, 0, 0),
			Port: 5000,
		}, dErr)
		if err != nil {
			t.Fatal(err)
		}
		aErr := NewAllocator(Options{Conn: pErr})
		if _, err := aErr.New(tuple, timeout, nil); errors.Cause(err) != dErr.err {
			t.Errorf("unexpected error: %s", err)
		}
	})
	t.Run("BadProto", func(t *testing.T) {
		if _, err := a.New(turn.FiveTuple{
			Client: client,
			Server: server,
			Proto:  1,
		}, timeout, nil); err == nil {
			t.Error("should error")
		}
	})
	expectedAddr := turn.Addr{
		Port: 5101,
		IP:   allocateIP,
	}
	if !expectedAddr.Equal(relayedAddr) {
		t.Errorf("unexpected relayed addr: %s", relayedAddr)
	}
	// Creating allocation and two permissions.
	if _, err = a.New(tuple, timeout, nil); err != ErrAllocationMismatch {
		t.Error("New() with same tuple should return mismatch error")
	}
	if err := a.ChannelBind(tuple, n, peer, now.Add(time.Second*5)); err != nil {
		t.Error(err)
	}
	if err := a.ChannelBind(tuple, n2, peer2, now.Add(time.Second*18)); err != nil {
		t.Error(err)
	}
	a.Prune(now)
	// Refreshing first permission to T+8.
	if err := a.ChannelBind(tuple, n, peer, now.Add(time.Second*8)); err != nil {
		t.Error(err)
	}
	// Collecting at T+7.
	a.Prune(now.Add(time.Second * 7))
	// Checking that both permissions still active.
	if _, err := a.SendBound(tuple, n, make([]byte, 100)); err != nil {
		t.Error(err)
	}
	if _, err := a.Send(tuple, peer2, make([]byte, 100)); err != nil {
		t.Error(err)
	}
	// Collecting T+9. First permission should expire.
	a.Prune(now.Add(time.Second * 9))
	if _, err := a.SendBound(tuple, n, make([]byte, 100)); err != ErrPermissionNotFound {
		t.Errorf("unexpected err: %v", err)
	}
	if _, err := a.SendBound(tuple, n2, make([]byte, 100)); err != nil {
		t.Error(err)
	}
	// Collecting T+17. Entire allocation expires.
	// Both permissions should expire too.
	a.Prune(now.Add(time.Second * 17))
	if _, err := a.SendBound(tuple, n, make([]byte, 100)); err != ErrPermissionNotFound {
		t.Errorf("unexpected err: %v", err)
	}
	if _, err := a.SendBound(tuple, n, make([]byte, 100)); err != ErrPermissionNotFound {
		t.Errorf("unexpected err: %v", err)
	}
	// Attempt to create a permission with expired allocation should
	// result to allocation mismatch.
	if err := a.ChannelBind(tuple, n, peer, now.Add(time.Second*10)); err != ErrAllocationMismatch {
		t.Error("unexpected allocation error, should be ErrAllocationNotFound")
	}
	// Re-creating allocation with same tuple should now succeed.
	relayedAddr, err = a.New(tuple, timeout, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectedAddr = turn.Addr{
		Port: 5102,
		IP:   allocateIP,
	}
	if !expectedAddr.Equal(relayedAddr) {
		t.Errorf("unexpected relayed addr: %s", relayedAddr)
	}
	a.Remove(tuple)
}
