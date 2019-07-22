// Package filter implements address filtering.
package filter

import (
	"net"

	"gortc.io/turn"
)

// Action is possible action that can be applied to address.
type Action byte

var actionToStr = map[Action]string{
	Pass:  "pass",
	Allow: "allow",
	Deny:  "deny",
}

func (a Action) String() string {
	return actionToStr[a]
}

// Possible action list.
const (
	Pass Action = iota
	Allow
	Deny
)

type subnetRule struct {
	action Action
	net    *net.IPNet
}

func (r subnetRule) Action(addr turn.Addr) Action {
	inSubnet := r.net.Contains(addr.IP)
	if inSubnet {
		return r.action
	}
	return Pass
}

// AllowNet allows any address from subnet.
func AllowNet(subnet string) (Rule, error) {
	return StaticNetRule(Allow, subnet)
}

// ForbidNet blocks any address from subnet.
func ForbidNet(subnet string) (Rule, error) {
	return StaticNetRule(Deny, subnet)
}

// StaticNetRule returns static rule for provided subnet that will apply
// "allow" to it.
func StaticNetRule(action Action, subnet string) (Rule, error) {
	_, parsedNet, err := net.ParseCIDR(subnet)
	if err != nil {
		return nil, err
	}
	return subnetRule{action: action, net: parsedNet}, nil
}

type allowAll struct{}

func (allowAll) Action(addr turn.Addr) Action { return Allow }

// AllowAll is Rule that always returns Allow.
var AllowAll Rule = allowAll{}

// Rule represents filtering rule.
type Rule interface {
	Action(addr turn.Addr) Action
}

// List is list of rules with default action.
type List struct {
	action Action
	rules  []Rule
}

// Action implements Rule.
//
// Returns first matched rule from list or default action if none found.
// Matched is rule that returned Allow or Deny action (not "Pass").
func (f *List) Action(addr turn.Addr) Action {
	for i := range f.rules {
		a := f.rules[i].Action(addr)
		if a == Pass {
			continue
		}
		return a
	}
	return f.action
}

// NewFilter initializes and returns new List with provided default action
// and rule list.
func NewFilter(action Action, rules ...Rule) *List { return &List{rules: rules, action: action} }
