package main

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/gortc/stun"
	"github.com/gortc/turn"
)

type echoHandler struct {
	l    *zap.Logger
	echo chan struct{}
}

func (h *echoHandler) HandleEvent(e stun.Event) {
	if e.Error != nil {
		h.l.Fatal("error", zap.Error(e.Error))
	}
	h.echo <- struct{}{}
}

var rootCmd = &cobra.Command{
	Use: "gortcd-turn-client",
	Run: func(cmd *cobra.Command, args []string) {
		logCfg := zap.NewDevelopmentConfig()
		logCfg.DisableCaller = true
		logCfg.DisableStacktrace = true
		start := time.Now()
		logCfg.EncoderConfig.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			d := int64(time.Since(start).Nanoseconds() / 1e6)
			enc.AppendString(fmt.Sprintf("%04d", d))
		}
		logger, err := logCfg.Build()
		echoAddr, err := net.ResolveUDPAddr("udp", viper.GetString("peer.addr"))
		if err != nil {
			logger.Fatal("failed to parse", zap.Error(err))
		}

		if viper.GetBool("peer") {
			logger.Info("running in peer mode")
			laddr, err := net.ResolveUDPAddr("udp", viper.GetString("peer.listen"))
			if err != nil {
				logger.Fatal("failed to resolve UDP addr", zap.Error(err))
			}
			c, err := net.ListenUDP("udp", laddr)
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
			return
		}
		conn, err := net.Dial("udp", viper.GetString("server"))
		if err != nil {
			logger.Fatal("failed to dial", zap.Error(err))
		}
		h := &echoHandler{
			l:    logger,
			echo: make(chan struct{}),
		}
		c, err := stun.NewClient(stun.ClientOptions{
			Connection: conn,
			Agent: stun.NewAgent(stun.AgentOptions{
				Handler: h,
			}),
		})
		if err != nil {
			logger.Fatal("failed to create client", zap.Error(err))
		}
		defer c.Close()
		var (
			reladdr turn.RelayedAddress
			maddr   stun.XORMappedAddress
		)
		if err := c.Do(stun.MustBuild(
			stun.TransactionID,
			turn.AllocateRequest,
			turn.RequestedTransportUDP,
		), time.Now().Add(time.Second), func(event stun.Event) {
			if event.Error != nil {
				logger.Fatal("failed to allocate", zap.Error(event.Error))
			}
			logger.Info("got", zap.Stringer("m", event.Message))
			if err := event.Message.Parse(&reladdr, &maddr); err != nil {
				logger.Fatal("failed to parse allocation", zap.Error(err))
			}
			logger.Debug("got allocation",
				zap.Stringer("reladdr", reladdr),
				zap.Stringer("maddr", maddr),
			)
		}); err != nil {
			logger.Fatal("failed to Do()", zap.Error(err))
		}
		peerAddr := turn.PeerAddress{
			IP:   echoAddr.IP,
			Port: echoAddr.Port,
		}
		if err := c.Do(stun.MustBuild(
			stun.TransactionID,
			turn.CreatePermissionRequest,
			peerAddr, stun.Fingerprint,
		), time.Now().Add(time.Second), func(event stun.Event) {
			if event.Error != nil {
				logger.Fatal("failed to allocate", zap.Error(event.Error))
			}
			logger.Info("got", zap.Stringer("m", event.Message))
		}); err != nil {
			logger.Fatal("failed to Do()", zap.Error(err))
		}
		var (
			sentData = turn.Data("Hello world!")
		)
		if err := c.Indicate(stun.MustBuild(
			stun.TransactionID,
			turn.SendIndication,
			peerAddr, sentData, stun.Fingerprint,
		)); err != nil {
			logger.Fatal("failed to indicate", zap.Error(err))
		}
		logger.Info("sent indication")
		select {
		case <-h.echo:
			logger.Info("ok")
		case <-time.After(time.Second):
			logger.Fatal("timed out")
		}
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	{
		f := rootCmd.Flags()
		f.StringP("server", "s", "localhost:3478", "server addr")
		f.StringP("client", "c", "0.0.0.0:40001", "client addr")
		f.String("peer.addr", "0.0.0.0:40002", "peer addr for client")
		f.BoolP("peer", "p", false, "peer mode")
		f.String("peer.listen", "0.0.0.0:40002", "peer addr")

		viper.BindPFlag("server", f.Lookup("server"))
		viper.BindPFlag("client", f.Lookup("client"))
		viper.BindPFlag("peer.addr", f.Lookup("peer.addr"))
		viper.BindPFlag("peer", f.Lookup("peer"))
		viper.BindPFlag("peer.listen", f.Lookup("peer.listen"))
	}
}

func main() {
	Execute()
}
