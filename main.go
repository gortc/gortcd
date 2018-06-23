package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

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
	allocs       []Allocation

	ip          net.IP
	currentPort int
	conn        net.PacketConn
}

var (
	software          = stun.NewSoftware("gortc/gortcd")
	errNotSTUNMessage = errors.New("not stun message")
)

func (s *Server) dealloc(client Addr) {
	var (
		newAllocs []Allocation
		toClose   []net.Conn
	)

	s.allocsMux.Lock()
	for _, a := range s.allocs {
		if !a.Tuple.Client.Equal(client) {
			newAllocs = append(newAllocs, a)
			continue
		}
		for _, p := range a.Permissions {
			toClose = append(toClose, p.Conn)
		}
	}
	n := copy(s.allocs, newAllocs)
	s.allocs = s.allocs[:n]
	s.allocsMux.Unlock()

	for _, c := range toClose {
		if err := c.Close(); err != nil {
			s.log.Warn("failed to close conn",
				zap.Error(err),
			)
		}
	}
}

func (s *Server) collect(t time.Time) {
	var (
		newAllocs []Allocation
		toClose   []net.Conn
	)

	s.allocsMux.Lock()
	for _, a := range s.allocs {
		var newPermissions []Permission
		for _, p := range a.Permissions {
			if p.Lifetime.After(t) {
				newPermissions = append(newPermissions, p)
				continue
			}
			toClose = append(toClose, p.Conn)
		}
		n := copy(a.Permissions, newPermissions)
		a.Permissions = a.Permissions[:n]
		if n > 0 {
			newAllocs = append(newAllocs, a)
		}
	}
	n := copy(s.allocs, newAllocs)
	s.allocs = s.allocs[:n]
	s.allocsMux.Unlock()

	for _, c := range toClose {
		if err := c.Close(); err != nil {
			s.log.Warn("failed to close conn",
				zap.Error(err),
			)
		}
	}
}

var (
	bindingSuccess = stun.NewType(stun.MethodBinding, stun.ClassSuccessResponse)
	allocSuccess   = stun.NewType(stun.MethodAllocate, stun.ClassSuccessResponse)
)

func (s *Server) sendByPermission(
	data turn.Data,
	client Addr,
	addr turn.PeerAddress,
) error {
	var (
		conn net.Conn
	)

	s.log.Info("searching for allocation",
		zap.Stringer("client", client),
		zap.Stringer("addr", addr),
	)
	s.allocsMux.Lock()
	for _, a := range s.allocs {
		if !a.Tuple.Client.Equal(client) {
			continue
		}
		// Got client allocation.
		allowed := false
		s.log.Info("searching for permission",
			zap.Int("len", len(a.Permissions)),
		)
		for _, p := range a.Permissions {
			s.log.Debug("comparing",
				zap.Stringer("a", Addr(addr)),
				zap.Stringer("b", p.Addr),
			)
			if !Addr(addr).Equal(p.Addr) {
				continue
			}
			allowed = true
			conn = p.Conn
		}
		if !allowed {
			s.allocsMux.Unlock()
			return errors.Errorf("not allowed: %s", addr)
		}
	}
	s.allocsMux.Unlock()

	if conn != nil {
		_, err := conn.Write(data)
		if err != nil {
			return errors.Wrap(err, "failed to write")
		}
	}
	return nil
}

func (s *Server) HandlePeerData(d []byte, t FiveTuple, a Addr) {
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
	l.Info("sent", zap.Stringer("m", m))
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
	client := Addr{
		Port: port,
		IP:   ip,
	}
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
				stun.NewRealm("realm"),
				stun.NewNonce("nonce"),
				stun.CodeUnauthorised,
				stun.Fingerprint,
			)
		}
		s.log.Info("got allocate", zap.Stringer("realm", realm))
		s.allocsMux.Lock()
		tuple := FiveTuple{
			Client: Addr{
				Port: port,
				IP:   ip,
			},
			Server: Addr{
				IP:   s.ip,
				Port: s.currentPort,
			},
			Proto: transport.Protocol,
		}
		s.allocs = append(s.allocs, Allocation{
			Tuple:    tuple,
			Callback: s,
		})
		s.currentPort++
		s.allocsMux.Unlock()

		return res.Build(req, allocSuccess,
			&stun.XORMappedAddress{
				IP:   tuple.Client.IP,
				Port: tuple.Client.Port,
			},
			&turn.RelayedAddress{
				IP:   tuple.Server.IP,
				Port: tuple.Server.Port,
			},
			stun.Fingerprint,
		)
	case turn.CreatePermissionRequest:
		var (
			addr turn.PeerAddress
		)
		if err := req.Parse(&addr); err != nil {
			return errors.Wrap(err, "failed to parse send indication")
		}
		s.log.Info("processing create permission request")
		permission := Permission{
			Lifetime: time.Now().Add(time.Minute * 5),
			Addr:     Addr(addr),
			Log: s.log.With(
				zap.Stringer("client", client),
				zap.Stringer("addr", Addr(addr)),
			),
		}
		s.allocsMux.Lock()
		for i, a := range s.allocs {
			if !a.Tuple.Client.Equal(client) {
				continue
			}
			switch a.Tuple.Proto {
			case turn.ProtoUDP:
				// TODO: Don't lock on this.
				conn, err := net.DialUDP("udp", &net.UDPAddr{
					Port: a.Tuple.Server.Port,
					IP:   a.Tuple.Server.IP,
				}, &net.UDPAddr{
					Port: addr.Port,
					IP:   addr.IP,
				})
				if err != nil {
					s.allocsMux.Unlock()
					return errors.Wrap(err, "failed to dial")
				}
				s.log.Info("created permission connection", zap.Stringer("t", a.Tuple))
				permission.Conn = conn
			default:
				s.allocsMux.Unlock()
				return errors.Errorf("proto %s not implemented", a.Tuple.Proto)
			}
			a.Permissions = append(a.Permissions, permission)
			go a.ReadUntilClosed(permission)
			s.allocs[i] = a
		}
		s.allocsMux.Unlock()
		return res.Build(req,
			stun.NewType(stun.MethodCreatePermission, stun.ClassSuccessResponse),
		)
	case turn.RefreshRequest:
		var (
			addr     turn.PeerAddress
			lifetime turn.Lifetime
			deAlloc  bool
		)
		if err := req.Parse(&addr); err != nil && err != stun.ErrAttributeNotFound {
			return errors.Wrap(err, "failed to parse refresh request")
		}
		if err := req.Parse(&addr); err != nil {
			if err != stun.ErrAttributeNotFound {
				return errors.Wrap(err, "failed to parse")
			}
		}
		deAlloc = lifetime.Duration == 0
		if deAlloc {
			s.dealloc(client)
		} else {
			t := time.Now()
			s.allocsMux.Lock()
			for _, a := range s.allocs {
				if !a.Tuple.Client.Equal(client) {
					continue
				}
				for i := range a.Permissions {
					p := a.Permissions[i]
					if !Addr(addr).Equal(p.Addr) {
						continue
					}
					p.Lifetime = t
					a.Permissions[i] = p
				}
			}
			s.allocsMux.Unlock()
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
		res.Reset()
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
		log:         logger,
		ip:          net.IPv4(0, 0, 0, 0),
		currentPort: 50000,
		conn:        c,
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
