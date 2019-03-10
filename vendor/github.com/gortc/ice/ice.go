// Package ice implements RFC 8445
// Interactive Connectivity Establishment (ICE):
// A Protocol for Network Address Translator (NAT) Traversal
package ice

import (
	"encoding/binary"
)

// bin is shorthand for BigEndian.
var bin = binary.BigEndian
