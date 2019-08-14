package server

import (
	"bytes"
	"fmt"
	"net"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gortc.io/turnc"

	"gortc.io/gortcd/internal/auth"
	"gortc.io/gortcd/internal/testutil"
)

func TestServerIntegration(t *testing.T) {
	// Test is same as e2e/gortc-turn.
	const (
		username = "username"
		password = "password"
		realm    = "realm"
	)
	echoConn, echoUDPAddr := listenUDP(t)
	serverConn, serverUDPAddr := listenUDP(t)
	serverCore, serverLogs := observer.New(zap.DebugLevel)
	defer testutil.EnsureNoErrors(t, serverLogs)
	s, err := New(Options{
		Log:   zap.New(serverCore),
		Conn:  serverConn,
		Realm: realm,
		Auth: auth.NewStatic([]auth.StaticCredential{
			{Username: username, Password: password, Realm: realm},
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
		for {
			buf := make([]byte, 1024)
			n, addr, err := echoConn.ReadFromUDP(buf)
			if err != nil {
				t.Errorf("peer: failed to read: %v", err)
			}
			t.Logf("peer: got message: %s", string(buf[:n]))
			if _, err := echoConn.WriteToUDP(buf[:n], addr); err != nil {
				t.Errorf("peer: failed to write back: %v", err)
			}
			t.Logf("peer: echoed back")
		}
	}()
	go func() {
		if err := s.Serve(); err != nil {
			t.Error(err)
		}
	}()
	// Creating connection from client to server.
	c, err := net.DialUDP("udp", nil, serverUDPAddr)
	if err != nil {
		t.Fatalf("failed to dial to TURN server: %v", err)
	}
	client, err := turnc.New(turnc.Options{
		Conn:     c,
		Username: username,
		Password: password,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	a, err := client.Allocate()
	if err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	p, err := a.Create(echoUDPAddr.IP)
	if err != nil {
		t.Fatalf("failed to create permission: %v", err)
	}
	conn, err := p.CreateUDP(echoUDPAddr)
	if err != nil {
		t.Fatal(err)
	}
	// Sending and receiving "hello" message.
	if _, err := fmt.Fprint(conn, "hello"); err != nil {
		t.Fatal("failed to write data")
	}
	sent := []byte("hello")
	got := make([]byte, len(sent))
	if _, err = conn.Read(got); err != nil {
		t.Fatalf("failed to read data: %v", err)
	}
	if !bytes.Equal(got, sent) {
		t.Fatal("got incorrect data")
	}
	// Repeating via channel binding.
	for i := range got {
		got[i] = 0
	}
	if bindErr := conn.Bind(); bindErr != nil {
		t.Fatal("failed to bind", zap.Error(err))
	}
	if !conn.Bound() {
		t.Fatal("should be bound")
	}
	t.Logf("bound to channel: 0x%x", int(conn.Binding()))
	if _, err := fmt.Fprint(conn, "hello"); err != nil {
		t.Fatalf("failed to write data: %v", err)
	}
	if _, err = conn.Read(got); err != nil {
		t.Fatalf("failed to read data: %v", err)
	}
	t.Logf("client: got message: %s", string(got))
	if !bytes.Equal(got, sent) {
		t.Error("data mismatch")
	}
}
