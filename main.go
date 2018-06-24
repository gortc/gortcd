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

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/gortc/gortcd/internal/allocator"
	"github.com/gortc/ice"
	"github.com/gortc/stun"
	"github.com/gortc/turn"
)

var (
	network     = flag.String("net", "udp", "network to listen")
	address     = flag.String("addr", fmt.Sprintf("0.0.0.0:%d", stun.DefaultPort), "address to listen")
	profile     = flag.Bool("profile", false, "run pprof")
	checkRealm  = flag.Bool("check-realm", false, "check realm")
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
	allocsMux    sync.Mutex
	allocs       *allocator.Allocator
	ip           net.IP
	currentPort  int64
	conn         net.PacketConn
}

var (
	software          = stun.NewSoftware("gortc/gortcd")
	errNotSTUNMessage = errors.New("not stun message")
)

func (s *Server) collect(t time.Time) {
	s.allocs.Collect(t)
}

var (
	bindingSuccess = stun.NewType(stun.MethodBinding, stun.ClassSuccessResponse)
	allocSuccess   = stun.NewType(stun.MethodAllocate, stun.ClassSuccessResponse)
)

func (s *Server) sendByPermission(
	data turn.Data,
	client allocator.Addr,
	addr turn.PeerAddress,
) error {
	s.log.Info("searching for allocation",
		zap.Stringer("client", client),
		zap.Stringer("addr", addr),
	)
	_, err := s.allocs.Send(client, allocator.Addr(addr), data)
	if err != nil {
		return err
	}
	return err
}

func (s *Server) HandlePeerData(d []byte, t allocator.FiveTuple, a allocator.Addr) {
	destination := &net.UDPAddr{
		IP:   t.Client.IP,
		Port: t.Client.Port,
	}
	l := s.log.With(
		zap.Stringer("t", t),
		zap.Stringer("addr", a),
		zap.Int("len", len(d)),
		zap.Stringer("d", destination),
	)
	l.Info("got peer data")
	s.conn.SetWriteDeadline(time.Now().Add(time.Second))
	m := stun.New()
	if err := m.Build(
		stun.TransactionID,
		stun.NewType(stun.MethodData, stun.ClassIndication),
		turn.Data(d),
		stun.Fingerprint,
	); err != nil {
		l.Error("failed to build", zap.Error(err))
		return
	}
	if _, err := s.conn.WriteTo(m.Raw, destination); err != nil {
		l.Error("failed to write", zap.Error(err))
	}
	l.Info("sent data from peer", zap.Stringer("m", m))
}

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
	client := allocator.Addr{
		Port: port,
		IP:   ip,
	}
	now := time.Now()
	s.log.Info("got message",
		zap.Stringer("m", req),
		zap.Stringer("addr", client),
	)
	switch req.Type {
	case stun.BindingRequest:
		return res.Build(req, bindingSuccess,
			software,
			&stun.XORMappedAddress{
				IP:   ip,
				Port: port,
			},
			stun.Fingerprint,
		)
	case turn.AllocateRequest:
		var (
			transport turn.RequestedTransport
			realm     stun.Realm
		)
		if err := transport.GetFrom(req); err != nil {
			return errors.Wrap(err, "failed to get requested transport")
		}
		s.log.Info("processing allocate request")
		if err := realm.GetFrom(req); err != nil && *checkRealm {
			return res.Build(req, stun.NewType(stun.MethodAllocate, stun.ClassErrorResponse),
				stun.CodeUnauthorised,
				stun.Fingerprint,
			)
		}
		s.log.Info("got allocate", zap.Stringer("realm", realm))
		server, err := s.allocs.New(
			client, transport.Protocol, s,
		)
		if err != nil {
			s.log.Error("failed to allocate", zap.Error(err))
			return res.Build(req, stun.NewType(stun.MethodAllocate, stun.ClassErrorResponse),
				stun.CodeServerError,
				stun.Fingerprint,
			)
		}
		return res.Build(req, allocSuccess,
			(*stun.XORMappedAddress)(&server),
			(*turn.RelayedAddress)(&client),
			stun.Fingerprint,
		)
	case turn.CreatePermissionRequest:
		var (
			addr     turn.PeerAddress
			lifetime turn.Lifetime
		)
		if err := addr.GetFrom(req); err != nil {
			return errors.Wrap(err, "failed to ger create permission request addr")
		}
		switch err := lifetime.GetFrom(req); err {
		case nil:
			if lifetime.Duration > time.Hour {
				// Requested lifetime is too big.
				return res.Build(req, stun.NewType(stun.MethodCreatePermission, stun.ClassErrorResponse),
					stun.CodeBadRequest,
					stun.Fingerprint,
				)
			}
		case stun.ErrAttributeNotFound:
			lifetime.Duration = time.Minute // default
		default:
			return errors.Wrap(err, "failed to get lifetime")
		}
		s.log.Info("processing create permission request")
		if err := s.allocs.CreatePermission(client, allocator.Addr(addr), now.Add(lifetime.Duration)); err != nil {
			return errors.Wrap(err, "failed to create allocation")
		}
		return res.Build(req,
			stun.NewType(stun.MethodCreatePermission, stun.ClassSuccessResponse),
		)
	case turn.RefreshRequest:
		var (
			addr     turn.PeerAddress
			lifetime turn.Lifetime
		)
		if err := req.Parse(&addr); err != nil && err != stun.ErrAttributeNotFound {
			return errors.Wrap(err, "failed to parse refresh request")
		}
		if err := req.Parse(&addr); err != nil {
			if err != stun.ErrAttributeNotFound {
				return errors.Wrap(err, "failed to parse")
			}
		}
		switch lifetime.Duration {
		case 0:
			s.allocs.Remove(client)
		default:
			t := now.Add(lifetime.Duration)
			s.allocs.Refresh(client, allocator.Addr(addr), t)
		}
		return res.Build(req,
			stun.NewType(stun.MethodRefresh, stun.ClassSuccessResponse),
		)
	case turn.SendIndication:
		var (
			data turn.Data
			addr turn.PeerAddress
		)
		if err := req.Parse(&data, &addr); err != nil {
			return errors.Wrap(err, "failed to parse send indication")
		}
		if err := s.sendByPermission(data, client, addr); err != nil {
			s.log.Warn("send failed",
				zap.Error(err),
			)
		}
		return nil
	default:
		return errors.Errorf("unknown request type %s", req.Type)
	}
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
	if len(res.Raw) == 0 {
		// Indication.
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
		}
		res.Reset()
		req.Reset()
	}
	return nil
}

// ListenUDPAndServe listens on laddr and process incoming packets.
func ListenUDPAndServe(serverNet, laddr string, logger *zap.Logger) error {
	c, err := net.ListenPacket(serverNet, laddr)
	if err != nil {
		return err
	}
	netAlloc, err := allocator.NewNetAllocator(
		logger.Named("port"), c.LocalAddr(), allocator.SystemPortAllocator{},
	)
	if err != nil {
		return err
	}
	s := &Server{
		log:         logger,
		ip:          net.IPv4(0, 0, 0, 0),
		currentPort: 50000,
		conn:        c,
		allocs:      allocator.NewAllocator(logger.Named("allocator"), netAlloc),
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
			l.Warn("picking addr from ICE")
			// Picking first addr from ice candidate.
			addrs, err := ice.DefaultGatherer.Gather()
			if err != nil {
				log.Fatal(err)
			}
			for _, a := range addrs {
				l.Warn("got", zap.Stringer("a", a))
			}
			firstAddr := addrs[len(addrs)-1]
			normalized = strings.Replace(normalized, "0.0.0.0", firstAddr.IP.String(), -1)
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
