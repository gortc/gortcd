package allocator

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gortc/turn"
)

func TestAddr_FromUDPAddr(t *testing.T) {
	u := &net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 1234,
	}
	a := new(Addr)
	a.FromUDPAddr(u)
	if !u.IP.Equal(a.IP) || u.Port != a.Port {
		t.Error("not equal")
	}
}

func TestFiveTuple_Equal(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b FiveTuple
		v    bool
	}{
		{
			name: "blank",
			v:    true,
		},
		{
			name: "proto",
			a: FiveTuple{
				Proto: turn.ProtoUDP,
			},
		},
		{
			name: "server",
			a: FiveTuple{
				Server: Addr{
					Port: 100,
				},
			},
		},
		{
			name: "client",
			a: FiveTuple{
				Client: Addr{
					Port: 100,
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if v := tc.a.Equal(tc.b); v != tc.v {
				t.Errorf("%s [%v!=%v] %s",
					tc.a, v, tc.v, tc.b,
				)
			}
		})
	}
}

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
