package server

import (
	"net"
	"testing"
	"time"

	"gortc.io/stun"

	"gortc.io/turn"
)

func TestServer_processAllocationRequest(t *testing.T) {
	s, stop := newServer(t)
	defer stop()
	var (
		username = stun.NewUsername("username")
		addr     = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
		peer     = turn.PeerAddress{
			Port: 1234,
			IP:   net.IPv4(88, 11, 22, 33),
		}
	)
	m := stun.MustBuild(stun.TransactionID, turn.AllocateRequest,
		username, peer, stun.Fingerprint,
	)
	ctx := &context{
		cfg:      s.config(),
		request:  new(stun.Message),
		response: new(stun.Message),
	}
	ctx.request.Raw = make([]byte, len(m.Raw))
	ctx.request.Raw = ctx.request.Raw[:len(m.Raw)]
	ctx.client = turn.Addr{
		IP:   addr.IP,
		Port: addr.Port,
	}
	ctx.proto = turn.ProtoUDP
	ctx.setTuple()
	copy(ctx.request.Raw, m.Raw)
	if err := s.process(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.response.TransactionID != m.TransactionID {
		t.Error("unexpected response transaction ID")
	}
	var (
		realm stun.Realm
		nonce stun.Nonce
	)
	if err := ctx.response.Parse(&realm, &nonce); err != nil {
		t.Fatal(err)
	}
	if len(realm) == 0 {
		t.Fatal("no realm")
	}
	t.Run("Success", func(t *testing.T) {
		i := stun.NewLongTermIntegrity("username", realm.String(), "secret")
		m = stun.MustBuild(stun.TransactionID, turn.AllocateRequest,
			turn.RequestedTransportUDP, username, realm, nonce, peer, i, stun.Fingerprint,
		)
		ctx.request.Raw = append(ctx.request.Raw[:0], m.Raw...)
		if err := s.process(ctx); err != nil {
			t.Fatal(err)
		}
		if ctx.response.Type.Class != stun.ClassSuccessResponse {
			var errCode stun.ErrorCodeAttribute
			errCode.GetFrom(ctx.response)
			t.Errorf("unexpected error %s: %s", errCode, ctx.response)
		}
		t.Run("Refresh", func(t *testing.T) {
			m = stun.MustBuild(stun.TransactionID, turn.RefreshRequest,
				turn.Lifetime{Duration: time.Minute * 10},
				username, realm, nonce, peer, i, stun.Fingerprint,
			)
			ctx.request.Raw = append(ctx.request.Raw[:0], m.Raw...)
			if err := s.process(ctx); err != nil {
				t.Fatal(err)
			}
			if ctx.response.Type.Class != stun.ClassSuccessResponse {
				var errCode stun.ErrorCodeAttribute
				errCode.GetFrom(ctx.response)
				t.Errorf("unexpected error %s: %s", errCode, ctx.response)
			}
			var lifetime turn.Lifetime
			if getErr := lifetime.GetFrom(ctx.response); getErr != nil {
				t.Error(getErr)
			}
			if lifetime.Duration != time.Minute*10 {
				t.Error("bad lifetime")
			}
		})
		t.Run("Dealloc", func(t *testing.T) {
			m = stun.MustBuild(stun.TransactionID, turn.RefreshRequest,
				turn.Lifetime{},
				username, realm, nonce, peer, i, stun.Fingerprint,
			)
			ctx.request.Raw = append(ctx.request.Raw[:0], m.Raw...)
			if err := s.process(ctx); err != nil {
				t.Fatal(err)
			}
			if ctx.response.Type.Class != stun.ClassSuccessResponse {
				var errCode stun.ErrorCodeAttribute
				errCode.GetFrom(ctx.response)
				t.Errorf("unexpected error %s: %s", errCode, ctx.response)
			}
		})
	})
	t.Run("BadIntegrity", func(t *testing.T) {
		i := stun.NewLongTermIntegrity("username", realm.String(), "secret111")
		m = stun.MustBuild(stun.TransactionID, turn.AllocateRequest,
			turn.RequestedTransportUDP, username, realm, nonce, peer, i, stun.Fingerprint,
		)
		ctx.request.Raw = append(ctx.request.Raw[:0], m.Raw...)
		if err := s.process(ctx); err != nil {
			t.Fatal(err)
		}
		if ctx.response.Type.Class != stun.ClassErrorResponse {
			t.Errorf("unexpected response: %s", ctx.response)
		}
	})
	t.Run("UnexpectedMessageType", func(t *testing.T) {
		i := stun.NewLongTermIntegrity("username", realm.String(), "secret")
		m = stun.MustBuild(stun.TransactionID, stun.NewType(25, 1),
			turn.RequestedTransportUDP, username, realm, nonce, peer, i, stun.Fingerprint,
		)
		ctx.request.Raw = append(ctx.request.Raw[:0], m.Raw...)
		if err := s.process(ctx); err != nil {
			t.Fatal(err)
		}
		if ctx.response.Type.Class != stun.ClassErrorResponse {
			t.Errorf("unexpected response: %s", ctx.response)
		}
	})
}
