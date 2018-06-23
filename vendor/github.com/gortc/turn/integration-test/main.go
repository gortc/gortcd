package main

import (
	"bytes"
	"flag"
	"fmt"
	"net"
	"time"

	"github.com/gortc/stun"
	"github.com/gortc/turn"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	udp      = "udp"
	peerPort = 56780
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
	}
	logger.Info("got message",
		zap.Stringer("m", res),
		zap.Stringer("t", res.Type),
		zap.Duration("rtt", time.Since(start)),
	)
	return nil
}

func main() {
	flag.Parse()
	var (
		serverAddr *net.UDPAddr
		echoAddr   *net.UDPAddr
		code       stun.ErrorCodeAttribute
		req        = new(stun.Message)
		res        = new(stun.Message)
	)
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

	if flag.Arg(0) == "peer" {
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

	for i := 0; i < 10; i++ {
		serverAddr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("turn-server:%d", turn.DefaultPort))
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond * 300 * time.Duration(i))
	}
	if err != nil {
		panic(err)
	}
	for i := 0; i < 10; i++ {
		echoAddr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("turn-peer:%d", peerPort))
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond * 300 * time.Duration(i))
	}
	if err != nil {
		panic(err)
	}

	c, err := net.DialUDP(udp, nil, serverAddr)
	if err != nil {
		logger.Fatal("failed to dial to TURN server",
			zap.Error(err),
		)
	}
	logger.Info("dial server",
		zap.Stringer("laddr", c.LocalAddr()),
		zap.Stringer("raddr", c.RemoteAddr()),
	)

	// Constructing allocate request with integrity
	if err := do(logger, req, res, c,
		stun.TransactionID,
		turn.AllocateRequest,
		turn.RequestedTransportUDP,
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
		IP:   echoAddr.IP,
		Port: echoAddr.Port,
	}
	logger.Info("peer address", zap.Stringer("addr", peerAddr))
	if err := do(logger, req, res, c, stun.TransactionID,
		turn.CreatePermissionRequest,
		peerAddr,
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
		sentData,
		peerAddr,
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
		turn.RefreshRequest,
		turn.ZeroLifetime,
	); err != nil {
		logger.Fatal("failed to do", zap.Error(err))
	}
	if isErr(res) {
		code.GetFrom(res)
		logger.Fatal("got error response", zap.Stringer("err", code))
	}
	logger.Info("closing")
}
