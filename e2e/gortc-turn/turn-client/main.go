package main

import (
	"bytes"
	"flag"
	"fmt"
	"net"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/gortc/turn"
)

const (
	udp      = "udp"
	peerPort = 56780
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
		resolved, resolveErr = net.ResolveUDPAddr(udp, addr)
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
		d := int64(time.Since(start).Nanoseconds() / 1e6)
		enc.AppendString(fmt.Sprintf("%04d", d))
	}
	logger, err := logCfg.Build()
	if err != nil {
		panic(err)
	}
	return logger
}

func runPeer(logger *zap.Logger) {
	laddr, err := net.ResolveUDPAddr(udp, fmt.Sprintf(":%d", peerPort))
	if err != nil {
		logger.Fatal("failed to resolve UDP addr", zap.Error(err))
	}
	c, err := net.ListenUDP(udp, laddr)
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}
	logger.Info("listening as echo server", zap.Stringer("laddr", c.LocalAddr()))
	for {
		// Starting echo server.
		buf := make([]byte, 1024)
		n, addr, err := c.ReadFromUDP(buf)
		if err != nil {
			logger.Fatal("failed to read", zap.Error(err))
		}
		logger.Info("got message",
			zap.String("body", string(buf[:n])),
			zap.Stringer("raddr", addr),
		)
		// Echoing back.
		if _, err := c.WriteToUDP(buf[:n], addr); err != nil {
			logger.Fatal("failed to write back", zap.Error(err))
		}
		logger.Info("echoed back",
			zap.Stringer("raddr", addr),
		)
	}
}

func main() {
	flag.Parse()
	logger := newLogger()
	if flag.Arg(0) == "peer" {
		runPeer(logger)
	}
	// Resolving server and peer addresses.
	var (
		serverAddr = resolve("turn-server", turn.DefaultPort)
		echoAddr   = resolve("turn-peer", peerPort)
	)
	// Creating connection from client to server.
	c, err := net.DialUDP(udp, nil, serverAddr)
	if err != nil {
		logger.Fatal("failed to dial to TURN server", zap.Error(err))
	}
	logger.Info("dialed server",
		zap.Stringer("laddr", c.LocalAddr()),
		zap.Stringer("raddr", c.RemoteAddr()),
		zap.Stringer("peer", echoAddr),
	)
	client, err := turn.NewClient(turn.ClientOptions{
		Log:      logger.Named("client"),
		Conn:     c,
		Username: "user",
		Password: "secret",
	})
	if err != nil {
		logger.Fatal("failed to create client", zap.Error(err))
	}
	a, err := client.Allocate()
	if err != nil {
		logger.Fatal("failed to create allocation", zap.Error(err))
	}
	p, err := a.Create(echoAddr)
	if err != nil {
		logger.Fatal("failed to create permission", zap.Error(err))
	}
	// Sending and receiving "hello" message.
	if _, err := fmt.Fprint(p, "hello"); err != nil {
		logger.Fatal("failed to write data")
	}
	sent := []byte("hello")
	got := make([]byte, len(sent))
	if _, err = p.Read(got); err != nil {
		logger.Fatal("failed to read data", zap.Error(err))
	}
	if !bytes.Equal(got, sent) {
		logger.Fatal("got incorrect data")
	}
	// Repeating via channel binding.
	for i := range got {
		got[i] = 0
	}
	if bindErr := p.Bind(); bindErr != nil {
		logger.Fatal("failed to bind", zap.Error(err))
	}
	if !p.Bound() {
		logger.Fatal("should be bound")
	}
	logger.Info("bound to channel",
		turn.ZapChannelNumber("number", p.Binding()),
	)
	// Sending and receiving "hello" message.
	if _, err := fmt.Fprint(p, "hello"); err != nil {
		logger.Fatal("failed to write data")
	}
	if _, err = p.Read(got); err != nil {
		logger.Fatal("failed to read data", zap.Error(err))
	}
	if !bytes.Equal(got, sent) {
		logger.Fatal("got incorrect data")
	}
	logger.Info("closing")
}
