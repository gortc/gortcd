package ice

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net"

	ct "github.com/gortc/ice/candidate"
)

// Addr represents transport address, the combination of an IP address
// and the transport protocol (such as UDP or TCP) port.
type Addr struct {
	IP    net.IP
	Port  int
	Proto ct.Protocol
}

// Equal returns true of b equals to a.
func (a Addr) Equal(b Addr) bool {
	if a.Proto != b.Proto {
		return false
	}
	if a.Port != b.Port {
		return false
	}
	return a.IP.Equal(b.IP)
}

func (a Addr) String() string {
	return fmt.Sprintf("%s:%d/%s", a.IP, a.Port, a.Proto)
}

// Candidates is list of candidates ordered by priority descending.
type Candidates []Candidate

func (c Candidates) Len() int           { return len(c) }
func (c Candidates) Less(i, j int) bool { return c[i].Priority > c[j].Priority }
func (c Candidates) Swap(i, j int)      { c[i], c[j] = c[j], c[i] }

// The Candidate is a transport address that is a potential point of contact
// for receipt of data. Candidates also have properties — their type
// (server reflexive, relayed, or host), priority, foundation, and base.
type Candidate struct {
	Addr        Addr
	Type        ct.Type
	Priority    int
	Foundation  []byte
	Base        Addr
	Related     Addr
	ComponentID int
}

// Equal reports whether c equals to b.
func (c Candidate) Equal(b Candidate) bool {
	if c.Type != b.Type {
		return false
	}
	if c.Priority != b.Priority {
		return false
	}
	if !c.Addr.Equal(b.Addr) {
		return false
	}
	if !bytes.Equal(c.Foundation, b.Foundation) {
		return false
	}
	if !c.Base.Equal(b.Base) {
		return false
	}
	if !c.Related.Equal(b.Related) {
		// Should we skip that check?
		return false
	}
	if c.ComponentID != b.ComponentID {
		return false
	}
	return true
}

const foundationLength = 8

// Foundation computes foundation value for candidate. The serverAddr parameter
// is for STUN or TURN server address, zero value is valid. Will return nil if
// candidate is nil.
//
// Value is an arbitrary string used in the freezing algorithm to
// group similar candidates. It is the same for two candidates that
// have the same type, base IP address, protocol (UDP, TCP, etc.),
// and STUN or TURN server. If any of these are different, then the
// foundation will be different.
func Foundation(c *Candidate, serverAddr Addr) []byte {
	if c == nil {
		return nil
	}
	h := sha256.New()
	values := [][]byte{
		{byte(c.Type)},
		c.Base.IP,
		{byte(c.Addr.Proto)},
	}
	if len(serverAddr.IP) > 0 {
		values = append(values,
			serverAddr.IP,
			[]byte{byte(serverAddr.Proto)},
		)
	}
	h.Write(bytes.Join(values, []byte{':'})) // #nosec
	return h.Sum(nil)[:foundationLength]
}

// The RECOMMENDED values for type preferences are 126 for host
// candidates, 110 for peer-reflexive candidates, 100 for server-
// reflexive candidates, and 0 for relayed candidates.
//
// From RFC 8445 Section 5.1.2.2.
var typePreferences = map[ct.Type]int{
	ct.Host:            126,
	ct.PeerReflexive:   110,
	ct.ServerReflexive: 100,
	ct.Relayed:         0,
}

// TypePreference returns recommended type preference for candidate type.
func TypePreference(t ct.Type) int { return typePreferences[t] }

// Priority calculates the priority value by RFC 8445 Section 5.1.2.1 formulae.
//
// The typePref value MUST be an integer from 0 (lowest preference) to 126
// (highest preference) inclusive, MUST be identical for all candidates of
// the same type, and MUST be different for candidates of different types.
//
// The localPref value MUST be an integer from 0 (lowest preference) to
// 65535 (highest preference) inclusive. When there is only a single IP
// address, this value SHOULD be set to 65535. If there are multiple
// candidates for a particular component for a particular data stream
// that have the same type, the local preference MUST be unique for each
// one. If an ICE agent is dual stack, the local preference SHOULD be
// set according to the current best practice described in [RFC8421].
//
// The component ID MUST be an integer between 1 and 256 inclusive.
func Priority(typePref, localPref, componentID int) int {
	// priority = (2^24)*(type preference) +
	//	(2^8)*(local preference) +
	//	(2^0)*(256 - component ID)
	return (1<<24)*typePref + (1<<8)*localPref + (1<<0)*(256-componentID)
}
