// Package candidate contains common types for ice candidate.
package candidate

// Type encodes the type of candidate. This specification
// defines the values "host", "srflx", "prflx", and "relay" for host,
// server reflexive, peer reflexive, and relayed candidates,
// respectively. The set of candidate types is extensible for the
// future.
type Type byte

// Set of possible candidate types.
const (
	// Host is a candidate obtained by binding to a specific port
	// from an IP address on the host.  This includes IP addresses on
	// physical interfaces and logical ones, such as ones obtained
	// through VPNs.
	Host Type = iota
	// ServerReflexive is a candidate whose IP address and port
	// are a binding allocated by a NAT for an ICE agent after it sends a
	// packet through the NAT to a server, such as a STUN server.
	ServerReflexive
	// PeerReflexive is a candidate whose IP address and port are
	// a binding allocated by a NAT for an ICE agent after it sends a
	// packet through the NAT to its peer.
	PeerReflexive
	// Relayed is a candidate obtained from a relay server, such as
	// a TURN server.
	Relayed
)

var candidateTypeToStr = map[Type]string{
	Host:            "host",
	ServerReflexive: "server-reflexive",
	PeerReflexive:   "peer-reflexive",
	Relayed:         "relayed",
}

func strOrUnknown(str string) string {
	if len(str) == 0 {
		return "unknown"
	}
	return str
}

func (c Type) String() string {
	return strOrUnknown(candidateTypeToStr[c])
}

// Protocol is protocol for address.
type Protocol byte

// Supported protocols.
const (
	UDP Protocol = iota
	ProtocolUnknown
)

func (t Protocol) String() string {
	switch t {
	case UDP:
		return "UDP"
	default:
		return "Unknown"
	}
}
