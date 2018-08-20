package peer

import (
	"net"
	"sync"

	"github.com/gortc/turn"
)

// Action is possible action that can be applied to address.
type Action byte

// Possible action list.
const (
	Pass Action = iota
	Allow
	Forbid
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
	return StaticNetRule(Forbid, subnet)
}

// StaticNetRule returns static rule for provided subnet that will apply
// "allow" to it.
func StaticNetRule(action Action, subnet string) (Rule, error) {
	_, parsedNet, err := net.ParseCIDR(subnet)
	if err != nil {
		return nil, err
	}
	return subnetRule{
		action: action,
		net:    parsedNet,
	}, nil
}

type allowAll struct{}

func (allowAll) Action(addr turn.Addr) Action { return Allow }

// AllowAll is Rule that always returns true.
var AllowAll Rule = allowAll{}

// Rule represents filtering rule.
type Rule interface {
	Action(addr turn.Addr) Action
}

// Filter is list of rules with default action.
type Filter struct {
	action  Action
	ruleMux sync.RWMutex
	rules   []Rule
}

// SetAction replaces current default action.
func (f *Filter) SetAction(action Action) {
	f.ruleMux.Lock()
	f.action = action
	f.ruleMux.Unlock()
}

// SetRules replaces current rule set with provided one.
func (f *Filter) SetRules(rules []Rule) {
	f.ruleMux.Lock()
	f.rules = append(f.rules[:0], rules...)
	f.ruleMux.Unlock()
}

// Action implements Rule.
func (f *Filter) Action(addr turn.Addr) Action {
	f.ruleMux.RLock()
	defer f.ruleMux.RUnlock()
	for i := range f.rules {
		a := f.rules[i].Action(addr)
		if a == Pass {
			continue
		}
		return a
	}
	return f.action
}

// NewFilter initializes and returns new Filter with provided default action
// and rule list.
func NewFilter(action Action, rules ...Rule) *Filter {
	return &Filter{
		rules:  rules,
		action: action,
	}
}
