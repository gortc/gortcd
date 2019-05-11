package gather

import (
	"bytes"
	"fmt"
	"net"
	"sort"

	"github.com/gortc/ice/internal"
)

const precedencesCount = 11

var precedences [precedencesCount]precedenceConfig

type precedenceConfig struct {
	ipNet *net.IPNet
	value int
}

// Gatherer is source for addresses.
//
// See RFC 8445 Section 2.1 for details on gathering.
type Gatherer interface {
	Gather() ([]Addr, error)
}

func init() {
	// Initializing policy table.
	// See RFC 6724 Section 2.1.
	/*
	   ::1/128               50     0
	   ::/0                  40     1
	   ::ffff:0:0/96         35     4
	   2002::/16             30     2
	   2001::/32              5     5
	   fc00::/7               3    13
	   ::/96                  1     3
	   fec0::/10              1    11
	   3ffe::/16              1    12
	*/
	for i, p := range [precedencesCount]struct {
		cidr  string
		value int
		label int
	}{
		{"::1/128", 50, 0},
		{"127.0.0.1/8", 45, 0},
		{"::/0", 40, 1},
		{"::ffff:0:0/96", 35, 4},
		{"fe80::/10", 33, 1},
		{"2002::/16", 30, 2},
		{"2001::/32", 5, 5},
		{"fc00::/7", 3, 13},
		{"::/96", 1, 3},
		{"fec0::/10", 1, 11},
		{"3ffe::/16", 1, 12},
	} {
		precedences[i] = precedenceConfig{
			ipNet: internal.MustParseNet(p.cidr),
			value: p.value,
		}
	}
}

// Addr represents gathered address from interface.
type Addr struct {
	IP         net.IP
	Zone       string
	Precedence int
}

// Addrs is addr slice helper.
type Addrs []Addr

func (s Addrs) Less(i, j int) bool {
	si, sj := s[i], s[j]
	if si.Precedence == sj.Precedence {
		// Comparing IP's to make stable sort.
		return bytes.Compare(si.IP, sj.IP) < 0
	}
	return si.Precedence > sj.Precedence
}

func (s Addrs) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s Addrs) Len() int {
	return len(s)
}

func (a Addr) String() string {
	if len(a.Zone) > 0 {
		return fmt.Sprintf("%s (zone %s) [%d]",
			a.IP, a.Zone, a.Precedence,
		)
	}
	return fmt.Sprintf("%s [%d]", a.IP, a.Precedence)
}

// ZeroPortAddr return address with "0" port.
func (a Addr) ZeroPortAddr() string {
	host := a.IP.String()
	if len(a.Zone) > 0 {
		host += "%" + a.Zone
	}
	return net.JoinHostPort(host, "0")
}

type defaultGatherer struct{}

func precedence(ip net.IP) int {
	for _, p := range precedences {
		if p.ipNet.Contains(ip) {
			return p.value
		}
	}
	return 0
}

type netInterface interface {
	Addrs() ([]net.Addr, error)
}

func ifaceToAddr(i netInterface, name string) ([]Addr, error) {
	var addrs []Addr
	netAddrs, err := i.Addrs()
	if err != nil {
		return addrs, err
	}
	for _, a := range netAddrs {
		ip, _, err := net.ParseCIDR(a.String())
		if err != nil {
			return addrs, err
		}
		addr := Addr{
			IP:         ip,
			Precedence: precedence(ip),
		}
		if ip.IsLinkLocalUnicast() {
			// Zone must be set for link-local addresses.
			addr.Zone = name
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

func (g defaultGatherer) Gather() ([]Addr, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	addrs := make([]Addr, 0, 10)
	for _, iface := range interfaces {
		ifaceAddrs, err := ifaceToAddr(&iface, iface.Name)
		if err != nil {
			return addrs, err
		}
		addrs = append(addrs, ifaceAddrs...)
	}
	sort.Sort(Addrs(addrs))
	return addrs, nil
}

// DefaultGatherer uses net.Interfaces to gather addresses.
var DefaultGatherer Gatherer = defaultGatherer{}
