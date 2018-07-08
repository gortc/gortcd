package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/gortc/gortcd/internal/auth"
	"github.com/gortc/gortcd/internal/server"
	"github.com/gortc/ice"
	"github.com/gortc/stun"
)

const defaultAuthList = "username:realm:secret,username2:realm:secret2"

var (
	network     = flag.String("net", "udp", "network to listen")
	address     = flag.String("addr", fmt.Sprintf("0.0.0.0:%d", stun.DefaultPort), "address to listen")
	profile     = flag.Bool("profile", false, "run pprof")
	profileAddr = flag.String("profile.addr", "localhost:6060", "address to listen for pprof")
	authList    = flag.String("auth", defaultAuthList, "long-term credentials")
)

// ListenUDPAndServe listens on laddr and process incoming packets.
func ListenUDPAndServe(serverNet, laddr string, logger *zap.Logger) error {
	c, err := net.ListenPacket(serverNet, laddr)
	if err != nil {
		return err
	}
	var credentials []auth.StaticCredential
	for _, s := range strings.Split(*authList, ",") {
		parts := strings.Split(s, ":")
		credentials = append(credentials, auth.StaticCredential{
			Username: parts[0],
			Realm:    parts[1],
			Password: parts[2],
		})
	}
	s, err := server.New(server.Options{
		Conn: c,
		Log:  logger,
		Auth: auth.NewStatic(credentials),
	})
	if err != nil {
		return err
	}
	return s.Serve()
}

func normalize(address string) string {
	if len(address) == 0 {
		address = "0.0.0.0"
	}
	if !strings.Contains(address, ":") {
		address = fmt.Sprintf("%s:%d", address, stun.DefaultPort)
	}
	return address
}

func main() {
	flag.Parse()
	logCfg := zap.NewDevelopmentConfig()
	logCfg.DisableCaller = true
	logCfg.DisableStacktrace = true
	start := time.Now()
	logCfg.EncoderConfig.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		d := int64(time.Since(start).Nanoseconds() / 1e6)
		enc.AppendString(fmt.Sprintf("%04d", d))
	}
	l, err := logCfg.Build()
	if err != nil {
		panic(err)
	}
	if *profile {
		pprofAddr := *profileAddr
		l.Warn("running pprof", zap.String("addr", pprofAddr))
		go func() {
			if err := http.ListenAndServe(pprofAddr, nil); err != nil {
				l.Error("pprof failed to listen",
					zap.String("addr", pprofAddr),
					zap.Error(err),
				)
			}
		}()
	}
	switch *network {
	case "udp":
		normalized := normalize(*address)
		if strings.HasPrefix(normalized, "0.0.0.0") {
			l.Warn("running on all interfaces")
			l.Warn("picking addr from ICE")
			addrs, err := ice.Gather()
			if err != nil {
				log.Fatal(err)
			}
			wg := new(sync.WaitGroup)
			for _, a := range addrs {
				l.Warn("got", zap.Stringer("a", a))
				if a.IP.IsLoopback() {
					continue
				}
				if a.IP.IsLinkLocalMulticast() || a.IP.IsLinkLocalUnicast() {
					continue
				}
				if a.IP.To4() == nil {
					continue
				}
				l.Warn("using", zap.Stringer("a", a))
				wg.Add(1)
				go func(addr string) {
					defer wg.Done()
					l.Info("gortc/gortcd listening",
						zap.String("addr", addr),
						zap.String("network", *network),
					)
					if err = ListenUDPAndServe(*network, addr, l); err != nil {
						l.Fatal("failed to listen", zap.Error(err))
					}
				}(strings.Replace(normalized, "0.0.0.0", a.IP.String(), -1))
			}
			wg.Wait()
			return
		}
		l.Info("gortc/gortcd listening",
			zap.String("addr", normalized),
			zap.String("network", *network),
		)
		if err = ListenUDPAndServe(*network, normalized, l); err != nil {
			l.Fatal("failed to listen", zap.Error(err))
		}
	default:
		l.Fatal("unsupported network",
			zap.String("network", *network),
		)
	}
}
