package auth

import (
	"net"
	"testing"
	"time"

	"gortc.io/stun"

	"gortc.io/turn"
)

func TestNonceAuth_Check(t *testing.T) {
	a := NewNonceAuth(time.Minute * 30)
	now := time.Now()
	t.Run("BlankNonce", func(t *testing.T) {
		n, err := a.Check(turn.FiveTuple{}, stun.Nonce{}, now)
		if err != ErrStaleNonce {
			t.Error(err)
		}
		if len(n) == 0 {
			t.Error("unexpected nonce length")
		}
	})
	tuple := turn.FiveTuple{
		Server: turn.Addr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: 1001,
		},
		Client: turn.Addr{
			IP:   net.IPv4(127, 0, 0, 2),
			Port: 2001,
		},
		Proto: turn.ProtoUDP,
	}
	realNonce, err := a.Check(tuple, nil, now)
	if err != ErrStaleNonce {
		t.Error(err)
	}
	if _, checkErr := a.Check(tuple, realNonce, now); checkErr != nil {
		t.Error(checkErr)
	}
	newNonce, checkErr := a.Check(tuple, realNonce, now.Add(time.Minute*31))
	if checkErr != ErrStaleNonce {
		t.Error(checkErr)
	}
	if _, checkErr := a.Check(tuple, newNonce, now.Add(time.Minute*31).Add(time.Minute)); checkErr != nil {
		t.Error(checkErr)
	}
	if _, checkErr := a.Check(tuple, realNonce, now.Add(time.Minute*31).Add(time.Minute)); checkErr != ErrStaleNonce {
		t.Error(checkErr)
	}
}
