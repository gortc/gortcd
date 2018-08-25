package allocator

import (
	"net"
	"testing"

	"crypto/rand"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestSystemPortPooledAllocator_AllocatePort(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	defer func() {
		if logs.Len() > 0 {
			t.Error("got errors in logs")
		}
		for _, l := range logs.All() {
			t.Log(l.Message)
		}
	}()
	a := &SystemPortPooledAllocator{
		log:     zap.New(core),
		ip:      net.IPv4(127, 0, 0, 1),
		network: "udp4",
		maxPort: 34010,
		minPort: 34000,
		rand:    rand.Reader,
	}
	if err := a.init(); err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	alloc, err := a.allocate()
	if err != nil {
		t.Fatal(err)
	}
	if err = alloc.Close(); err != nil {
		t.Fatal(err)
	}
}
