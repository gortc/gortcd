package allocator

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gortc/turn"
)

func TestFiveTuple_String(t *testing.T) {
	s := fmt.Sprint(FiveTuple{
		Proto: turn.ProtoUDP,
		Server: Addr{
			Port: 100,
			IP:   net.IPv4(127, 0, 0, 1),
		},
		Client: Addr{
			Port: 200,
			IP:   net.IPv4(127, 0, 0, 1),
		},
	})
	if s != "127.0.0.1:200->127.0.0.1:100 (UDP)" {
		t.Error("unexpected stringer output")
	}
}

func TestPermission_String(t *testing.T) {
	s := fmt.Sprint(Permission{
		Addr: Addr{
			Port: 100,
			IP:   net.IPv4(127, 0, 0, 1),
		},
		Timeout: time.Date(2017, 1, 1, 1, 1, 1, 1, time.UTC),
	})
	if s != "127.0.0.1:100 [2017-01-01T01:01:01Z]" {
		t.Error("unexpected stringer output")
	}
}
