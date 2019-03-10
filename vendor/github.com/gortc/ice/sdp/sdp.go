// Package sdp implements SDP parsing for ICE.
package sdp

import (
	"bytes"
	"fmt"
	"net"
	"strconv"
	"unsafe"

	c "github.com/gortc/ice/candidate"
	"github.com/pkg/errors"
)

// Address represents address that can be ipv4/6 or FQDN.
type Address struct {
	Host []byte
	IP   net.IP
	Type c.AddressType
}

// reset sets all fields to zero values.
func (a *Address) reset() {
	a.Host = a.Host[:0]
	for i := range a.IP {
		a.IP[i] = 0
	}
	a.Type = c.AddressIPv4
}

// Equal returns true if b equals to a.
func (a Address) Equal(b Address) bool {
	if a.Type != b.Type {
		return false
	}
	switch a.Type {
	case c.AddressFQDN:
		return bytes.Equal(a.Host, b.Host)
	default:
		return a.IP.Equal(b.IP)
	}
}

func (a Address) str() string {
	switch a.Type {
	case c.AddressFQDN:
		return string(a.Host)
	default:
		return a.IP.String()
	}
}

func (a Address) String() string {
	return a.str()
}

const (
	sdpCandidateHost            = "host"
	sdpCandidateServerReflexive = "srflx"
	sdpCandidatePeerReflexive   = "prflx"
	sdpCandidateRelay           = "relay"
)

// Candidate is parsed ICE candidate from SDP.
//
// This attribute is used with Interactive Connectivity
// Establishment (ICE), and provides one of many possible candidate
// addresses for communication. These addresses are validated with
// an end-to-end connectivity check using Session Traversal Utilities
// for NAT (STUN)).
type Candidate struct {
	ConnectionAddress Address
	Port              int
	Transport         c.TransportType
	TransportValue    []byte
	Foundation        int
	ComponentID       int
	Priority          int
	Type              c.Type
	RelatedAddress    Address
	RelatedPort       int

	// Extended attributes
	NetworkCost int
	Generation  int

	// Other attributes
	Attributes Attributes
}

// Reset sets all fields to zero values.
func (ct *Candidate) Reset() {
	ct.ConnectionAddress.reset()
	ct.RelatedAddress.reset()
	ct.RelatedPort = 0
	ct.NetworkCost = 0
	ct.Generation = 0
	ct.Transport = c.TransportUnknown
	ct.TransportValue = ct.TransportValue[:0]
	ct.Attributes = ct.Attributes[:0]
}

// Equal returns true if b candidate is equal to c.
func (ct Candidate) Equal(b *Candidate) bool {
	if !ct.ConnectionAddress.Equal(b.ConnectionAddress) {
		return false
	}
	if ct.Port != b.Port {
		return false
	}
	if ct.Transport != b.Transport {
		return false
	}
	if !bytes.Equal(ct.TransportValue, b.TransportValue) {
		return false
	}
	if ct.Foundation != b.Foundation {
		return false
	}
	if ct.ComponentID != b.ComponentID {
		return false
	}
	if ct.Priority != b.Priority {
		return false
	}
	if ct.Type != b.Type {
		return false
	}
	if ct.NetworkCost != b.NetworkCost {
		return false
	}
	if ct.Generation != b.Generation {
		return false
	}
	if !ct.Attributes.Equal(b.Attributes) {
		return false
	}
	return true
}

// Attribute is key-value pair.
type Attribute struct {
	Key   []byte
	Value []byte
}

// Attributes is list of attributes.
type Attributes []Attribute

// Value returns first attribute value with key k or
// nil of none found.
func (a Attributes) Value(k []byte) []byte {
	for _, attribute := range a {
		if bytes.Equal(attribute.Key, k) {
			return attribute.Value
		}
	}
	return nil
}

// Equal returns true if a equals b.
func (a Attributes) Equal(b Attributes) bool {
	if len(a) != len(b) {
		return false
	}
	for _, attr := range a {
		v := b.Value(attr.Key)
		if !bytes.Equal(v, attr.Value) {
			return false
		}
	}
	for _, attr := range b {
		v := a.Value(attr.Key)
		if !bytes.Equal(v, attr.Value) {
			return false
		}
	}
	return true
}

func byteStr(b []byte) string {
	if b == nil {
		return "<nil>"
	}
	return string(b)
}

func (a Attribute) String() string {
	return fmt.Sprintf("%v:%v", byteStr(a.Key), byteStr(a.Value))
}

// candidateParser should parse []byte into Candidate.
//
// a=candidate:3862931549 1 udp 2113937151 192.168.1.2 56032 typ host generation 0 network-cost 50
//     foundation ---┘    |  |      |            |         |
//   component id --------┘  |      |            |         |
//      transport -----------┘      |            |         |
//       priority ------------------┘            |         |
//  conn. address -------------------------------┘         |
//           port -----------------------------------------┘
type candidateParser struct {
	buf []byte
	c   *Candidate
}

const sp = ' '

const (
	mandatoryElements = 6
)

func parseInt(v []byte) (int, error) {
	return strconv.Atoi(b2s(v))
}

func (p *candidateParser) parseFoundation(v []byte) error {
	i, err := parseInt(v)
	if err != nil {
		return errors.Wrap(err, "failed to parse foundation")
	}
	p.c.Foundation = i
	return nil
}

func (p *candidateParser) parseComponentID(v []byte) error {
	i, err := parseInt(v)
	if err != nil {
		return errors.Wrap(err, "failed to parse component ID")
	}
	p.c.ComponentID = i
	return nil
}

func (p *candidateParser) parsePriority(v []byte) error {
	i, err := parseInt(v)
	if err != nil {
		return errors.Wrap(err, "failed to parse priority")
	}
	p.c.Priority = i
	return nil
}

func (p *candidateParser) parsePort(v []byte) error {
	i, err := parseInt(v)
	if err != nil {
		return errors.Wrap(err, "failed to parse port")
	}
	p.c.Port = i
	return nil
}

func (p *candidateParser) parseRelatedPort(v []byte) error {
	i, err := parseInt(v)
	if err != nil {
		return errors.Wrap(err, "failed to parse port")
	}
	p.c.RelatedPort = i
	return nil
}

// b2s converts byte slice to a string without memory allocation.
//
// Note it may break if string and/or slice header will change
// in the future go versions.
func b2s(b []byte) string {
	return *(*string)(unsafe.Pointer(&b)) // #nosec
}

func parseIP(dst net.IP, v []byte) net.IP {
	ip := net.ParseIP(b2s(v))
	return append(dst, ip...)
}

func (candidateParser) parseAddress(v []byte, target *Address) error {
	target.IP = parseIP(target.IP, v)
	if target.IP == nil {
		target.Host = v
		target.Type = c.AddressFQDN
		return nil
	}
	target.Type = c.AddressIPv6
	if target.IP.To4() != nil {
		target.Type = c.AddressIPv4
	}
	return nil
}

func (p *candidateParser) parseConnectionAddress(v []byte) error {
	return p.parseAddress(v, &p.c.ConnectionAddress)
}

func (p *candidateParser) parseRelatedAddress(v []byte) error {
	return p.parseAddress(v, &p.c.RelatedAddress)
}

func (p *candidateParser) parseTransport(v []byte) error {
	if bytes.Equal(v, []byte("udp")) || bytes.Equal(v, []byte("UDP")) {
		p.c.Transport = c.TransportUDP
	} else {
		p.c.Transport = c.TransportUnknown
		p.c.TransportValue = v
	}
	return nil
}

// possible attribute keys.
const (
	aGeneration     = "generation"
	aNetworkCost    = "network-cost"
	aType           = "typ"
	aRelatedAddress = "raddr"
	aRelatedPort    = "rport"
)

func (p *candidateParser) parseAttribute(a Attribute) error {
	switch string(a.Key) {
	case aGeneration:
		return p.parseGeneration(a.Value)
	case aNetworkCost:
		return p.parseNetworkCost(a.Value)
	case aType:
		return p.parseType(a.Value)
	case aRelatedAddress:
		return p.parseRelatedAddress(a.Value)
	case aRelatedPort:
		return p.parseRelatedPort(a.Value)
	default:
		p.c.Attributes = append(p.c.Attributes, a)
		return nil
	}
}

type parseFn func(v []byte) error

const (
	minBufLen = 10
)

// parse populates internal Candidate from buffer.
func (p *candidateParser) parse() error {
	if len(p.buf) < minBufLen {
		return errors.Errorf("buffer too small (%d < %d)", len(p.buf), minBufLen)
	}
	// special cases for raw value support:
	if p.buf[0] == 'a' {
		p.buf = bytes.TrimPrefix(p.buf, []byte("a="))
	}
	if p.buf[0] == 'c' {
		p.buf = bytes.TrimPrefix(p.buf, []byte("candidate:"))
	}
	// pos is current position
	// l is value length
	// last is last character offset
	// of mandatory elements
	var pos, l, last int
	fns := [...]parseFn{
		p.parseFoundation,        // 0
		p.parseComponentID,       // 1
		p.parseTransport,         // 2
		p.parsePriority,          // 3
		p.parseConnectionAddress, // 4
		p.parsePort,              // 5
	}
	for i, b := range p.buf {
		if pos > mandatoryElements-1 {
			// saving offset
			last = i
			break
		}
		if b != sp {
			// non-space character
			l++
			continue
		}
		// space character reached
		if err := fns[pos](p.buf[i-l : i]); err != nil {
			return errors.Wrapf(err, "failed to parse char %d, pos %d",
				i, pos,
			)
		}
		pos++ // next element
		l = 0 // reset length of element
	}
	if last == 0 {
		// no non-mandatory elements
		return nil
	}
	// offsets:
	var (
		start  int // key start
		end    int // key end
		vStart int // value start
	)
	// subslicing to simplify offset calculation
	buf := p.buf[last-1:]
	// saving every k:v pair ignoring spaces
	for i, b := range buf {
		if b != sp && i != len(buf)-1 {
			// b is non-space or end of buffer
			if start == 0 {
				// key not started
				start = i
				continue
			}
			if vStart == 0 && end != 0 {
				// value not started and key ended
				vStart = i
			}
			continue
		}
		// b is space or end of buf reached
		if start == 0 {
			// key not started, skipping
			continue
		}
		if end == 0 {
			// key ended, saving offset
			end = i
			continue
		}
		if vStart == 0 {
			// value not started, skipping
			continue
		}
		if i == len(buf)-1 && buf[len(buf)-1] != sp {
			// fix for end of buf
			i = len(buf)
		}
		// value ended, saving attribute
		a := Attribute{
			Key:   buf[start:end],
			Value: buf[vStart:i],
		}
		if err := p.parseAttribute(a); err != nil {
			return errors.Wrapf(err, "failed to parse attribute at char %d",
				i+last,
			)
		}
		// reset offset
		vStart = 0
		end = 0
		start = 0
	}
	return nil
}

func (p *candidateParser) parseNetworkCost(v []byte) error {
	i, err := parseInt(v)
	if err != nil {
		return errors.Wrap(err, "failed to parse network cost")
	}
	p.c.NetworkCost = i
	return nil
}

func (p *candidateParser) parseGeneration(v []byte) error {
	i, err := parseInt(v)
	if err != nil {
		return errors.Wrap(err, "failed to parse generation")
	}
	p.c.Generation = i
	return nil
}

func (p *candidateParser) parseType(v []byte) error {
	switch string(v) {
	case sdpCandidateHost:
		p.c.Type = c.Host
	case sdpCandidatePeerReflexive:
		p.c.Type = c.PeerReflexive
	case sdpCandidateRelay:
		p.c.Type = c.Relay
	case sdpCandidateServerReflexive:
		p.c.Type = c.ServerReflexive
	default:
		return errors.Errorf("unknown candidate %q", v)
	}
	return nil
}

// ParseAttribute parses v into c and returns error if any.
func ParseAttribute(v []byte, c *Candidate) error {
	p := candidateParser{
		buf: v,
		c:   c,
	}
	err := p.parse()
	return err
}
