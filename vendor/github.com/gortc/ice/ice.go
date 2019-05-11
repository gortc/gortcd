// Package ice implements RFC 8445
// Interactive Connectivity Establishment (ICE):
// A Protocol for Network Address Translator (NAT) Traversal
package ice

import "encoding/binary"

// bin is shorthand for BigEndian.
var bin = binary.BigEndian

// State represents the ICE agent state.
//
// As per RFC 8445 Section 6.1.3, the ICE agent has a state determined by the
// state of the checklists. The state is Completed if all checklists are
// Completed, Failed if all checklists are Failed, or Running otherwise.
type State byte

const (
	// Running if all checklists are nor completed not failed.
	Running State = iota
	// Completed if all checklists are completed.
	Completed
	// Failed if all checklists are failed.
	Failed
)

var stateToStr = map[State]string{
	Running:   "Running",
	Completed: "Completed",
	Failed:    "Failed",
}

func (s State) String() string { return stateToStr[s] }
