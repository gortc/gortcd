package server

import (
	"fmt"
	"net"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"gortc.io/stun"

	"gortc.io/gortcd/internal/auth"
	"gortc.io/gortcd/internal/testutil"
	"gortc.io/turn"
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
	s.start()
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

var cfgNoop = config{metrics: metricsNoop}

func TestServer_badRequest(t *testing.T) {
	s, stop := newServer(t)
	defer stop()
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
	m := stun.MustBuild(stun.BindingRequest, stun.Fingerprint)
	m.Raw = m.Raw[:len(m.Raw)-4]
	ctx := &context{
		request:  new(stun.Message),
		response: new(stun.Message),
		cfg:      cfgNoop,
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
		cfg:      cfgNoop,
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
