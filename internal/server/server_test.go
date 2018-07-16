package server

import (
	"bytes"
	"fmt"
	"net"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/gortc/gortcd/internal/allocator"
	"github.com/gortc/gortcd/internal/auth"
	"github.com/gortc/gortcd/internal/testutil"
	"github.com/gortc/stun"
	"github.com/gortc/turn"
)

func isErr(m *stun.Message) bool {
	return m.Type.Class == stun.ClassErrorResponse
}

func do(logger *zap.Logger, req, res *stun.Message, c *net.UDPConn, attrs ...stun.Setter) error {
	start := time.Now()
	if err := req.Build(attrs...); err != nil {
		logger.Error("failed to build", zap.Error(err))
		return err
	}
	if _, err := req.WriteTo(c); err != nil {
		logger.Error("failed to write",
			zap.Error(err), zap.Stringer("m", req),
		)
		return err
	}
	logger.Info("sent message", zap.Stringer("m", req), zap.Stringer("t", req.Type))
	if cap(res.Raw) < 800 {
		res.Raw = make([]byte, 0, 1024)
	}
	res.Reset()
	c.SetReadDeadline(time.Now().Add(time.Second * 2))
	_, err := res.ReadFrom(c)
	if err != nil {
		logger.Error("failed to read",
			zap.Error(err), zap.Stringer("m", req),
		)
		return err
	}
	if req.Type.Class != stun.ClassIndication && req.TransactionID != res.TransactionID {
		return fmt.Errorf("transaction ID mismatch: %x (got) != %x (expected)",
			req.TransactionID, res.TransactionID,
		)
	}
	logger.Info("got message",
		zap.Stringer("m", res),
		zap.Stringer("t", res.Type),
		zap.Duration("rtt", time.Since(start)),
	)
	return nil
}

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

func TestServerIntegration(t *testing.T) {
	echoConn, echoUDPAddr := listenUDP(t)
	serverConn, serverUDPAddr := listenUDP(t)
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{
		Log:  logger.Named("server"),
		Conn: serverConn,
		Auth: auth.NewStatic([]auth.StaticCredential{
			{Username: "username", Password: "secret", Realm: "realm"},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	go func() {
		logger.Info("listening as echo server", zap.Stringer("laddr", echoUDPAddr))
		for {
			// Starting echo server.
			buf := make([]byte, 1024)
			n, addr, err := echoConn.ReadFromUDP(buf)
			if err != nil {
				logger.Fatal("failed to read", zap.Error(err))
			}
			logger.Info("got message",
				zap.String("body", string(buf[:n])),
				zap.Stringer("raddr", addr),
			)
			// Echoing back.
			if _, err := echoConn.WriteToUDP(buf[:n], addr); err != nil {
				logger.Fatal("failed to write back", zap.Error(err))
			}
			logger.Info("echoed back",
				zap.Stringer("raddr", addr),
			)
		}
	}()
	go func() {
		// Starting server.
		if err := s.Serve(); err != nil {
			t.Error(err)
		}
	}()
	c, err := net.DialUDP("udp", nil, serverUDPAddr)
	if err != nil {
		logger.Fatal("failed to dial to TURN server",
			zap.Error(err),
		)
	}
	var (
		req      = stun.New()
		res      = stun.New()
		username = stun.NewUsername("username")
		password = "secret"
		code     stun.ErrorCodeAttribute
	)
	if err != nil {
		logger.Fatal("failed to dial to TURN server",
			zap.Error(err),
		)
	}
	logger.Info("dial server",
		zap.Stringer("laddr", c.LocalAddr()),
		zap.Stringer("raddr", c.RemoteAddr()),
	)

	// Constructing allocate request without integrity
	if err := do(logger, req, res, c,
		username,
		stun.TransactionID,
		turn.AllocateRequest,
		turn.RequestedTransportUDP,
	); err != nil {
		logger.Fatal("failed to do request", zap.Error(err))
	}
	if !isErr(res) {
		logger.Fatal("got no-error response")
	}
	var (
		nonce stun.Nonce
		realm stun.Realm
	)
	if err := res.Parse(&nonce, &realm); err != nil {
		logger.Fatal("failed to get nonce and realm")
	}
	integrity := stun.NewLongTermIntegrity(username.String(), realm.String(), password)
	// Constructing allocate request with integrity
	if err := do(logger, req, res, c,
		username, nonce, realm,
		stun.TransactionID,
		turn.AllocateRequest,
		turn.RequestedTransportUDP,
		integrity,
		stun.Fingerprint,
	); err != nil {
		logger.Fatal("failed to do request", zap.Error(err))
	}
	if isErr(res) {
		code.GetFrom(res)
		logger.Fatal("got error response", zap.Stringer("err", code))
	}

	// Decoding relayed and mapped address.
	var (
		reladdr turn.RelayedAddress
		maddr   stun.XORMappedAddress
	)
	if err := reladdr.GetFrom(res); err != nil {
		logger.Fatal("failed to get relayed address", zap.Error(err))
	}
	logger.Info("relayed address", zap.Stringer("addr", reladdr))
	if err := maddr.GetFrom(res); err != nil && err != stun.ErrAttributeNotFound {
		logger.Fatal("failed to decode relayed address", zap.Error(err))
	} else {
		logger.Info("mapped address", zap.Stringer("addr", maddr))
	}

	peerAddr := turn.PeerAddress{
		IP:   echoUDPAddr.IP,
		Port: echoUDPAddr.Port,
	}
	logger.Info("peer address", zap.Stringer("addr", peerAddr))
	if err := do(logger, req, res, c, stun.TransactionID,
		turn.CreatePermissionRequest,
		username, nonce, realm,
		peerAddr,
		integrity,
		stun.Fingerprint,
	); err != nil {
		logger.Fatal("failed to do request", zap.Error(err))
	}
	if isErr(res) {
		code.GetFrom(res)
		logger.Fatal("failed to allocate", zap.Stringer("err", code))
	}
	var (
		sentData = turn.Data("Hello world!")
	)
	// Allocation succeed.
	// Sending data to echo server.
	// can be as resetTo(type, attrs)?
	if err := do(logger, req, res, c, stun.TransactionID,
		turn.SendIndication,
		username, nonce, realm,
		sentData,
		peerAddr,
		integrity,
		stun.Fingerprint,
	); err != nil {
		logger.Fatal("failed to build", zap.Error(err))
	}
	logger.Info("sent data", zap.String("v", string(sentData)))
	if isErr(res) {
		code.GetFrom(res)
		logger.Fatal("got error response", zap.Stringer("err", code))
	}
	var data turn.Data
	if err := data.GetFrom(res); err != nil {
		logger.Fatal("failed to get DATA attribute", zap.Error(err))
	}
	logger.Info("got data", zap.String("v", string(data)))
	if bytes.Equal(data, sentData) {
		logger.Info("OK")
	} else {
		logger.Info("DATA mismatch")
	}

	// De-allocating.
	if err := do(logger, req, res, c, stun.TransactionID,
		username, nonce, realm,
		turn.RefreshRequest,
		turn.ZeroLifetime,
		integrity,
		stun.Fingerprint,
	); err != nil {
		logger.Fatal("failed to do", zap.Error(err))
	}
	if isErr(res) {
		code.GetFrom(res)
		logger.Fatal("got error response", zap.Stringer("err", code))
	}
	logger.Info("closing")
}

func TestServer_processBindingRequest(t *testing.T) {
	serverConn, _ := listenUDP(t)
	defer serverConn.Close()
	s, err := New(Options{
		Log:  zap.NewNop(),
		Conn: serverConn,
		Auth: auth.NewStatic([]auth.StaticCredential{
			{Username: "username", Password: "secret", Realm: "realm"},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	var (
		addr = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
	)
	m := stun.MustBuild(stun.BindingRequest, stun.Fingerprint)
	ctx := &context{
		request:  new(stun.Message),
		response: new(stun.Message),
	}
	ctx.request.Raw = make([]byte, len(m.Raw))
	ctx.request.Raw = ctx.request.Raw[:len(m.Raw)]
	ctx.client = allocator.Addr{
		IP:   addr.IP,
		Port: addr.Port,
	}
	copy(ctx.request.Raw, m.Raw)
	if err := s.process(ctx); err != nil {
		t.Fatal(err)
	}
	t.Run("ZeroAlloc", func(t *testing.T) {
		ctx.request.Raw = ctx.request.Raw[:len(m.Raw)]
		ctx.client = allocator.Addr{
			IP:   addr.IP,
			Port: addr.Port,
		}
		copy(ctx.request.Raw, m.Raw)
		testutil.ShouldNotAllocate(t, func() {
			s.process(ctx)
		})
	})
}

func BenchmarkServer_processBindingRequest(b *testing.B) {
	b.ReportAllocs()
	serverConn, _ := listenUDP(b)
	defer serverConn.Close()
	s, err := New(Options{
		Log:  zap.NewNop(),
		Conn: serverConn,
		Auth: auth.NewStatic([]auth.StaticCredential{
			{Username: "username", Password: "secret", Realm: "realm"},
		}),
	})
	if err != nil {
		b.Fatal(err)
	}
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
		ctx.client = allocator.Addr{
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
	serverConn, _ := listenUDP(t)
	defer serverConn.Close()
	s, err := New(Options{
		Log:  zap.NewNop(),
		Conn: serverConn,
		Auth: auth.NewStatic([]auth.StaticCredential{
			{Username: "username", Password: "secret", Realm: "realm"},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	var (
		addr = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
	)
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
	ctx.client = allocator.Addr{
		IP:   addr.IP,
		Port: addr.Port,
	}
	if err := s.process(ctx); err != errNotSTUNMessage {
		t.Fatal(err)
	}
	t.Run("ZeroAlloc", func(t *testing.T) {
		ctx.request.Raw = ctx.request.Raw[:len(buf)]
		copy(ctx.request.Raw, buf)
		ctx.client = allocator.Addr{
			IP:   addr.IP,
			Port: addr.Port,
		}
		testutil.ShouldNotAllocate(t, func() {
			s.process(ctx)
		})
	})
}
