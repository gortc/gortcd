package server

import (
	"fmt"
	"net"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/gortc/gortcd/internal/auth"
	"github.com/gortc/gortcd/internal/testutil"
	"github.com/gortc/stun"
	"github.com/gortc/turn"
)

func listenUDP(t testing.TB, addrs ...string) (*net.UDPConn, *net.UDPAddr) {
	addr := "127.0.0.1:0"
	if len(addrs) > 0 {
		addr = addrs[0]
	}
	rAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", rAddr)
	if err != nil {
		t.Fatal(err)
	}
	udpAddr, err := net.ResolveUDPAddr("udp", conn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	return conn, udpAddr
}

func newServer(t testing.TB, opts ...Options) (*Server, func()) {
	o := Options{
		Realm:    "realm",
		Software: "gortcd:test",
	}
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.Conn == nil {
		serverConn, _ := listenUDP(t)
		o.Conn = serverConn
	}
	if o.Workers == 0 {
		o.Workers = 1
	}
	if o.Auth == nil {
		o.Auth = auth.NewStatic([]auth.StaticCredential{
			{Username: "username", Password: "secret", Realm: "realm"},
		})
	}
	var logs *observer.ObservedLogs
	if o.Log == nil {
		core, newLogs := observer.New(zapcore.DebugLevel)
		logs = newLogs
		o.Log = zap.New(core)
	}
	s, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return s, func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
		if t.Failed() && logs != nil {
			encoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
			entries := logs.All()
			for i := range entries {
				buf, _ := encoder.EncodeEntry(
					entries[i].Entry, entries[i].Context,
				)
				fmt.Println(buf)
			}
		}
	}
}

func TestServer_processBindingRequest(t *testing.T) {
	s, stop := newServer(t)
	defer stop()
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
	m := stun.MustBuild(stun.BindingRequest, stun.Fingerprint)
	ctx := &context{
		cfg:      s.cfg.Load().(config),
		request:  new(stun.Message),
		response: new(stun.Message),
	}
	ctx.request.Raw = make([]byte, len(m.Raw))
	ctx.request.Raw = ctx.request.Raw[:len(m.Raw)]
	ctx.client = turn.Addr{
		IP:   addr.IP,
		Port: addr.Port,
	}
	copy(ctx.request.Raw, m.Raw)
	if err := s.process(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.response.Type != stun.BindingSuccess {
		t.Errorf("unexpected type: %s", ctx.response.Type)
	}
	t.Run("Software", func(t *testing.T) {
		var soft stun.Software
		if getErr := soft.GetFrom(ctx.response); getErr != nil {
			t.Fatal(getErr)
		}
		if soft.String() != "gortcd:test" {
			t.Errorf("bad software: %s", soft)
		}
	})
	t.Run("ZeroAlloc", func(t *testing.T) {
		s, stop := newServer(t, Options{
			Log: zap.NewNop(),
		})
		defer stop()
		addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
		m := stun.MustBuild(stun.BindingRequest, stun.Fingerprint)
		ctx := &context{
			cfg:      s.cfg.Load().(config),
			request:  new(stun.Message),
			response: new(stun.Message),
		}
		ctx.request.Raw = make([]byte, len(m.Raw))
		ctx.request.Raw = ctx.request.Raw[:len(m.Raw)]
		ctx.client = turn.Addr{
			IP:   addr.IP,
			Port: addr.Port,
		}
		copy(ctx.request.Raw, m.Raw)
		if err := s.process(ctx); err != nil {
			t.Fatal(err)
		}
		if ctx.response.Type != stun.BindingSuccess {
			t.Errorf("unexpected type: %s", ctx.response.Type)
		}
		ctx.request.Raw = ctx.request.Raw[:len(m.Raw)]
		ctx.client = turn.Addr{
			IP:   addr.IP,
			Port: addr.Port,
		}
		copy(ctx.request.Raw, m.Raw)
		testutil.ShouldNotAllocate(t, func() {
			s.process(ctx)
		})
	})
	t.Run("Auth", func(t *testing.T) {
		username := stun.NewUsername("username")
		ctx.cfg.authForSTUN = true
		ctx.request.Raw = ctx.request.Raw[:len(m.Raw)]
		ctx.client = turn.Addr{
			IP:   addr.IP,
			Port: addr.Port,
		}
		m = stun.MustBuild(stun.TransactionID, stun.BindingRequest, username, stun.Fingerprint)
		ctx.request.Raw = append(ctx.request.Raw[:0], m.Raw...)
		if err := s.process(ctx); err != nil {
			t.Fatal(err)
		}
		if ctx.response.Type != stun.BindingError {
			t.Errorf("unexpected response type: %s", ctx.response.Type)
		}
		var (
			realm stun.Realm
			nonce stun.Nonce
		)
		if err := ctx.response.Parse(&realm, &nonce); err != nil {
			t.Fatal(err)
		}
		t.Run("Success", func(t *testing.T) {
			i := stun.NewLongTermIntegrity("username", realm.String(), "secret")
			m = stun.MustBuild(stun.TransactionID, stun.BindingRequest,
				username, realm, nonce, i, stun.Fingerprint,
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
}

func TestServer_HandlePeerData(t *testing.T) {
	s, stop := newServer(t)
	defer stop()
	addr := turn.Addr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
	peerAddr := turn.Addr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
	now := time.Now()
	clientTuple := turn.FiveTuple{
		Client: addr,
		Proto:  turn.ProtoUDP,
	}
	timeout := now.Add(time.Minute)
	_, allocErr := s.allocs.New(clientTuple, timeout, s)
	if allocErr != nil {
		t.Fatal(allocErr)
	}
	t.Log("created allocation")
	if createErr := s.allocs.CreatePermission(clientTuple, peerAddr, timeout); createErr != nil {
		t.Fatal(createErr)
	}
	t.Log("created permission")
	if bindErr := s.allocs.ChannelBind(clientTuple, 0x4001, peerAddr, timeout); bindErr != nil {
		t.Fatal(bindErr)
	}
	t.Log("created binding")
	s.HandlePeerData([]byte("hello"), clientTuple, peerAddr)
}

func TestServer_processChannelData(t *testing.T) {
	s, stop := newServer(t)
	defer stop()
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
	m := &turn.ChannelData{
		Number: 0x4001,
		Data:   []byte{1, 2, 3, 4},
	}
	m.Encode()
	ctx := &context{
		request:  new(stun.Message),
		response: new(stun.Message),
		cdata:    new(turn.ChannelData),
	}
	ctx.request.Raw = make([]byte, len(m.Raw))
	ctx.request.Raw = ctx.request.Raw[:len(m.Raw)]
	ctx.client = turn.Addr{
		IP:   addr.IP,
		Port: addr.Port,
	}
	copy(ctx.request.Raw, m.Raw)
	if err := s.process(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ctx.response.Raw) != 0 {
		t.Error("unexpected response length")
	}
	t.Run("ZeroAlloc", func(t *testing.T) {
		s, stop := newServer(t, Options{
			Log: zap.NewNop(),
		})
		defer stop()
		addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
		m := &turn.ChannelData{
			Number: 0x4001,
			Data:   []byte{1, 2, 3, 4},
		}
		m.Encode()
		ctx := &context{
			request:  new(stun.Message),
			response: new(stun.Message),
			cdata:    new(turn.ChannelData),
		}
		ctx.request.Raw = make([]byte, len(m.Raw))
		ctx.request.Raw = ctx.request.Raw[:len(m.Raw)]
		ctx.client = turn.Addr{
			IP:   addr.IP,
			Port: addr.Port,
		}
		copy(ctx.request.Raw, m.Raw)
		if err := s.process(ctx); err != nil {
			t.Fatal(err)
		}
		if len(ctx.response.Raw) != 0 {
			t.Error("unexpected response length")
		}
		ctx.request.Raw = ctx.request.Raw[:len(m.Raw)]
		ctx.client = turn.Addr{
			IP:   addr.IP,
			Port: addr.Port,
		}
		copy(ctx.request.Raw, m.Raw)
		testutil.ShouldNotAllocate(t, func() {
			s.process(ctx)
		})
	})
}

type callbackNonceManager func(
	tuple turn.FiveTuple, value stun.Nonce, at time.Time,
) (stun.Nonce, error)

func (m callbackNonceManager) Check(
	tuple turn.FiveTuple, value stun.Nonce, at time.Time,
) (stun.Nonce, error) {
	return m(tuple, value, at)
}

func TestServer_processChannelBinding(t *testing.T) {
	s, stop := newServer(t, Options{
		Realm: "realm",
		NonceManager: callbackNonceManager(func(
			tuple turn.FiveTuple, value stun.Nonce, at time.Time,
		) (stun.Nonce, error) {
			return stun.NewNonce("nonce"), nil
		}),
	})
	defer stop()
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
	m := stun.MustBuild(stun.TransactionID, channelBindRequest,
		stun.NewNonce("nonce"),
		stun.NewUsername("username"),
		stun.NewRealm("realm"),
		&turn.PeerAddress{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: 1001,
		},
		turn.ChannelNumber(0x7070),
		stun.NewLongTermIntegrity("username", "realm", "secret"),
		stun.Fingerprint,
	)
	ctx := &context{
		cfg:      s.cfg.Load().(config),
		request:  new(stun.Message),
		response: new(stun.Message),
		cdata:    new(turn.ChannelData),
	}
	ctx.request.Raw = make([]byte, len(m.Raw))
	ctx.client = turn.Addr{
		IP:   addr.IP,
		Port: addr.Port,
	}
	ctx.setTuple()
	copy(ctx.request.Raw, m.Raw)
	if err := s.process(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.response.Type.Class == stun.ClassErrorResponse {
		var code stun.ErrorCodeAttribute
		code.GetFrom(ctx.response)
		if code.Code != stun.CodeAllocMismatch {
			t.Errorf("unexpected error: %s", code)
		}
	} else {
		t.Error("unexpected success")
	}
}

func BenchmarkServer_processBindingRequest(b *testing.B) {
	b.ReportAllocs()
	s, stop := newServer(b)
	defer stop()
	var (
		addr = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
	)
	m := stun.MustBuild(stun.BindingRequest, stun.Fingerprint)
	b.ResetTimer()
	ctx := &context{
		request:  new(stun.Message),
		response: new(stun.Message),
	}
	ctx.request.Raw = make([]byte, len(m.Raw))
	for i := 0; i < b.N; i++ {
		ctx.request.Raw = ctx.request.Raw[:len(m.Raw)]
		ctx.client = turn.Addr{
			IP:   addr.IP,
			Port: addr.Port,
		}
		copy(ctx.request.Raw, m.Raw)
		if err := s.process(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func TestServer_notStun(t *testing.T) {
	t.Run("Message", func(t *testing.T) {
		s, stop := newServer(t)
		defer stop()
		addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
		buf := make([]byte, 512)
		for i := range buf {
			buf[i] = byte(i % 127)
		}
		ctx := &context{
			request:  new(stun.Message),
			response: new(stun.Message),
		}
		ctx.request.Raw = make([]byte, len(buf), 1024)
		copy(ctx.request.Raw, buf)
		ctx.client = turn.Addr{
			IP:   addr.IP,
			Port: addr.Port,
		}
		if err := s.process(ctx); err != errNotSTUNMessage {
			t.Fatal(err)
		}
	})
	t.Run("ZeroAlloc", func(t *testing.T) {
		s, stop := newServer(t, Options{
			Log: zap.NewNop(),
		})
		defer stop()
		addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
		buf := make([]byte, 512)
		for i := range buf {
			buf[i] = byte(i % 127)
		}
		ctx := &context{
			request:  new(stun.Message),
			response: new(stun.Message),
		}
		ctx.request.Raw = make([]byte, len(buf), 1024)
		copy(ctx.request.Raw, buf)
		ctx.client = turn.Addr{
			IP:   addr.IP,
			Port: addr.Port,
		}
		testutil.ShouldNotAllocate(t, func() {
			s.process(ctx)
		})
	})
}

func TestServer_badRequest(t *testing.T) {
	s, stop := newServer(t)
	defer stop()
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
	m := stun.MustBuild(stun.BindingRequest, stun.Fingerprint)
	m.Raw = m.Raw[:len(m.Raw)-4]
	ctx := &context{
		request:  new(stun.Message),
		response: new(stun.Message),
	}
	ctx.request.Raw = make([]byte, len(m.Raw))
	ctx.request.Raw = ctx.request.Raw[:len(m.Raw)]
	ctx.client = turn.Addr{
		IP:   addr.IP,
		Port: addr.Port,
	}
	copy(ctx.request.Raw, m.Raw)
	if err := s.process(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.response.Length != 0 {
		t.Error("unexpected response")
	}
}

func TestServer_badFingerprint(t *testing.T) {
	s, stop := newServer(t)
	defer stop()
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
	m := stun.MustBuild(stun.BindingRequest)
	m.Add(stun.AttrFingerprint, []byte{1, 2, 3, 4})
	ctx := &context{
		request:  new(stun.Message),
		response: new(stun.Message),
	}
	ctx.request.Raw = make([]byte, len(m.Raw))
	ctx.request.Raw = ctx.request.Raw[:len(m.Raw)]
	ctx.client = turn.Addr{
		IP:   addr.IP,
		Port: addr.Port,
	}
	copy(ctx.request.Raw, m.Raw)
	if err := s.process(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.response.Type.Class != stun.ClassErrorResponse {
		t.Error("unexpected success")
	}
}

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
