package main

import (
	"flag"
	"fmt"
	"net"
	"time"

	"gortc.io/stun"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// resolve tries to resolve provided address multiple times, waiting
// between attempts and calling panic if it fails after 10 attempts.
func resolve(host string, port int) *net.UDPAddr {
	addr := fmt.Sprintf("%s:%d", host, port)
	var (
		resolved   *net.UDPAddr
		resolveErr error
	)
	for i := 0; i < 10; i++ {
		resolved, resolveErr = net.ResolveUDPAddr("udp", addr)
		if resolveErr == nil {
			return resolved
		}
		time.Sleep(time.Millisecond * 300 * time.Duration(i))
	}
	panic(resolveErr)
}

func newLogger() *zap.Logger {
	logCfg := zap.NewDevelopmentConfig()
	logCfg.DisableCaller = true
	logCfg.DisableStacktrace = true
	start := time.Now()
	logCfg.EncoderConfig.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		d := time.Since(start).Nanoseconds() / 1e6
		enc.AppendString(fmt.Sprintf("%04d", d))
	}
	logger, err := logCfg.Build()
	if err != nil {
		panic(err)
	}
	return logger
}

func main() {
	flag.Parse()
	l := newLogger().Sugar()
	// Resolving server address.
	var (
		serverAddr = resolve("turn-server", stun.DefaultPort)
	)
	// Creating connection from client to server.
	c, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		l.Fatalf("failed to dial to TURN server: %v", err)
	}
	l.Infow("dialed server",
		"laddr", c.LocalAddr(), "raddr", c.RemoteAddr(),
	)
	client, err := stun.NewClient(c)
	if err != nil {
		l.Fatalf("failed to create client: %v", err)
	}
	if err = client.Do(stun.MustBuild(stun.TransactionID, stun.BindingRequest), func(event stun.Event) {
		if event.Error != nil {
			l.Fatalf("event error: %v", event.Error)
		}
		l.Info(event.Message)
		if event.Message.Type != stun.BindingSuccess {
			l.Fatalf("unexpected type: %s", event.Message.Type)
		}
	}); err != nil {
		l.Fatal(err)
	}
	l.Info("closing")
}
