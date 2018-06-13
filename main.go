package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"strings"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/gortc/stun"
)

var (
	network     = flag.String("net", "udp", "network to listen")
	address     = flag.String("addr", fmt.Sprintf("0.0.0.0:%d", stun.DefaultPort), "address to listen")
	profile     = flag.Bool("profile", false, "run pprof")
	profileAddr = flag.String("profile.addr", "localhost:6060", "address to listen for pprof")
)

// Server is RFC 5389 basic server implementation.
//
// Current implementation is UDP only and not utilizes FINGERPRINT mechanism,
// nor ALTERNATE-SERVER, nor credentials mechanisms. It does not support
// backwards compatibility with RFC 3489.
type Server struct {
	Addr         string
	LogAllErrors bool
	log          *zap.Logger
}

var (
	software          = stun.NewSoftware("gortc/gortcd")
	errNotSTUNMessage = errors.New("not stun message")
)

func (s *Server) process(addr net.Addr, b []byte, req, res *stun.Message) error {
	if !stun.IsMessage(b) {
		s.log.Debug("not looks like stun message", zap.Stringer("addr", addr))
		return errNotSTUNMessage
	}
	if _, err := req.Write(b); err != nil {
		return errors.Wrap(err, "failed to read message")
	}
	var (
		ip   net.IP
		port int
	)
	switch a := addr.(type) {
	case *net.UDPAddr:
		ip = a.IP
		port = a.Port
	default:
		s.log.Error("unknown addr", zap.Stringer("addr", addr))
		return errors.Errorf("unknown addr %s", addr)
	}
	return res.Build(
		stun.NewTransactionIDSetter(req.TransactionID),
		stun.NewType(stun.MethodBinding, stun.ClassSuccessResponse),
		software,
		&stun.XORMappedAddress{
			IP:   ip,
			Port: port,
		},
		stun.Fingerprint,
	)
}

func (s *Server) serveConn(c net.PacketConn, res, req *stun.Message) error {
	if c == nil {
		return nil
	}
	buf := make([]byte, 1024)
	n, addr, err := c.ReadFrom(buf)
	if err != nil {
		s.log.Warn("readFrom failed", zap.Error(err))
		return nil
	}
	s.log.Debug("read",
		zap.Int("n", n),
		zap.Stringer("addr", addr),
	)
	if _, err = req.Write(buf[:n]); err != nil {
		s.log.Warn("write failed", zap.Error(err))
		return err
	}
	if err = s.process(addr, buf[:n], req, res); err != nil {
		if err == errNotSTUNMessage {
			return nil
		}
		s.log.Error("process failed", zap.Error(err))
		return nil
	}
	_, err = c.WriteTo(res.Raw, addr)
	if err != nil {
		s.log.Warn("writeTo failed", zap.Error(err))
	}
	return err
}

// Serve reads packets from connections and responds to BINDING requests.
func (s *Server) Serve(c net.PacketConn) error {
	var (
		res = new(stun.Message)
		req = new(stun.Message)
	)
	for {
		if err := s.serveConn(c, res, req); err != nil {
			s.log.Error("serveConn failed", zap.Error(err))
			return err
		}
		res.Reset()
		req.Reset()
	}
}

// ListenUDPAndServe listens on laddr and process incoming packets.
func ListenUDPAndServe(serverNet, laddr string, logger *zap.Logger) error {
	c, err := net.ListenPacket(serverNet, laddr)
	if err != nil {
		return err
	}
	s := &Server{
		log: logger,
	}
	return s.Serve(c)
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
	l, err := zap.NewDevelopment()
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
