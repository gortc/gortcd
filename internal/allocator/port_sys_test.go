package allocator

import (
	"testing"

	"gortc.io/turn"
)

func TestSystemPortAllocator_AllocatePort(t *testing.T) {
	a := SystemPortAllocator{}
	t.Run("Local", func(t *testing.T) {
		alloc, err := a.AllocatePort(turn.ProtoUDP, "udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		if err = alloc.Close(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("WithoutPort", func(t *testing.T) {
		_, err := a.AllocatePort(turn.ProtoUDP, "udp4", "127.0.0.1")
		if err == nil {
			t.Fatal("should not succeed")
		}
	})
	t.Run("Conflict", func(t *testing.T) {
		alloc, err := a.AllocatePort(turn.ProtoUDP, "udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer alloc.Close()
		_, err = a.AllocatePort(turn.ProtoUDP, "udp4", alloc.Addr.String())
		if err == nil {
			t.Error("should error")
		}
	})
}
