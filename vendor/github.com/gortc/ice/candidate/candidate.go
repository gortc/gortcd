// Package candidate contains common types for ice candidate.
package candidate

// AddressType is type for SDPConnectionAddress.
type AddressType byte

// Possible address types.
const (
	AddressIPv4 AddressType = iota
	AddressIPv6
	AddressFQDN
)

var addressTypeToStr = map[AddressType]string{
	AddressIPv4: "IPv4",
	AddressIPv6: "IPv6",
	AddressFQDN: "FQDN",
}

func (a AddressType) String() string {
	return strOrUnknown(addressTypeToStr[a])
}

// Type encodes the type of candidate. This specification
// defines the values "host", "srflx", "prflx", and "relay" for host,
// server reflexive, peer reflexive, and relayed candidates,
// respectively. The set of candidate types is extensible for the
// future.
type Type byte

// Set of candidate types.
const (
	Host            Type = iota // "host"
	ServerReflexive             // "srflx"
	PeerReflexive               // "prflx"
	Relay                       // "relay"
)

var candidateTypeToStr = map[Type]string{
	Host:            "host",
	ServerReflexive: "server-reflexive",
	PeerReflexive:   "peer-reflexive",
	Relay:           "relay",
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

// TransportType is transport type for candidate.
type TransportType byte

// Supported transport types.
const (
	TransportUDP TransportType = iota
	TransportUnknown
)

func (t TransportType) String() string {
	switch t {
	case TransportUDP:
		return "UDP"
	default:
		return "Unknown"
	}
}

