package filter

import (
	"net"
	"testing"

	"gortc.io/turn"
)

func TestAllowAll_Allowed(t *testing.T) {
	if AllowAll.Action(turn.Addr{}) != Allow {
		t.Error("should be allowed")
	}
}

func TestStaticNetRule(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		rule, err := StaticNetRule(Allow, "127.0.0.1/32")
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			Addr   turn.Addr
			Action Action
		}{
			{
				turn.Addr{IP: net.IPv4(127, 0, 0, 1)}, Allow,
			},
			{
				turn.Addr{IP: net.IPv4(127, 0, 0, 2)}, Pass,
			},
		} {
			t.Run(tc.Addr.String(), func(t *testing.T) {
				if rule.Action(tc.Addr) != tc.Action {
					t.Error("failed")
				}
			})
		}
	})
	t.Run("ParseError", func(t *testing.T) {
		if _, err := StaticNetRule(Allow, "bad"); err == nil {
			t.Error("should error")
		}
	})
}

func TestAllowNet(t *testing.T) {
	rule, err := AllowNet("192.168.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		Addr   turn.Addr
		Action Action
	}{
		{
			turn.Addr{IP: net.IPv4(192, 168, 0, 1)}, Allow,
		},
		{
			turn.Addr{IP: net.IPv4(127, 0, 0, 2)}, Pass,
		},
	} {
		t.Run(tc.Addr.String(), func(t *testing.T) {
			if rule.Action(tc.Addr) != tc.Action {
				t.Error("failed")
			}
		})
	}
}

func TestForbidNet(t *testing.T) {
	rule, err := ForbidNet("192.168.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		Addr   turn.Addr
		Action Action
	}{
		{
			turn.Addr{IP: net.IPv4(192, 168, 0, 1)}, Deny,
		},
		{
			turn.Addr{IP: net.IPv4(127, 0, 0, 2)}, Pass,
		},
	} {
		t.Run(tc.Addr.String(), func(t *testing.T) {
			if rule.Action(tc.Addr) != tc.Action {
				t.Error("failed")
			}
		})
	}
}

func TestFilter_Allowed(t *testing.T) {
	allowLoopback, err := AllowNet("127.0.0.1/32")
	if err != nil {
		t.Fatal(err)
	}
	forbidNet, err := ForbidNet("192.168.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	filter := NewFilter(Deny, allowLoopback, forbidNet)
	for _, tc := range []struct {
		Addr   turn.Addr
		Action Action
	}{
		{
			turn.Addr{IP: net.IPv4(192, 120, 0, 1)}, Deny,
		},
		{
			turn.Addr{IP: net.IPv4(192, 168, 0, 1)}, Deny,
		},
		{
			turn.Addr{IP: net.IPv4(127, 0, 0, 1)}, Allow,
		},
	} {
		t.Run(tc.Addr.String(), func(t *testing.T) {
			if filter.Action(tc.Addr) != tc.Action {
				t.Error("failed")
			}
		})
	}
	filter = NewFilter(Allow, forbidNet)
	for _, tc := range []struct {
		Addr   turn.Addr
		Action Action
	}{
		{
			turn.Addr{IP: net.IPv4(192, 120, 0, 1)}, Allow,
		},
		{
			turn.Addr{IP: net.IPv4(192, 168, 0, 1)}, Deny,
		},
		{
			turn.Addr{IP: net.IPv4(127, 0, 0, 1)}, Allow,
		},
	} {
		t.Run(tc.Addr.String(), func(t *testing.T) {
			if filter.Action(tc.Addr) != tc.Action {
				t.Error("failed")
			}
		})
	}
}
