package ice

import "sort"

// Checklist is set of pairs.
//
//
// From RFC 8455 Section 6.1.2:
//
// 	There is one checklist for each data stream.  To form a checklist,
// 	initiating and responding ICE agents form candidate pairs, compute
// 	pair priorities, order pairs by priority, prune pairs, remove lower-
// 	priority pairs, and set checklist states.  If candidates are added to
// 	a checklist (e.g., due to detection of peer-reflexive candidates),
// 	the agent will re-perform these steps for the updated checklist.
type Checklist struct {
	Pairs Pairs
	State ChecklistState
}

// ChecklistState represents the Checklist State.
//
// See RFC 8445 Section 6.1.2.1
type ChecklistState byte

var checklistStateToStr = map[ChecklistState]string{
	ChecklistRunning:   "Running",
	ChecklistCompleted: "Completed",
	ChecklistFailed:    "Failed",
}

func (s ChecklistState) String() string { return checklistStateToStr[s] }

const (
	// ChecklistRunning is neither Completed nor Failed yet. Checklists are
	// initially set to the Running state.
	ChecklistRunning ChecklistState = iota
	// ChecklistCompleted contains a nominated pair for each component of the
	// data stream.
	ChecklistCompleted
	// ChecklistFailed does not have a valid pair for each component of the data
	// stream, and all of the candidate pairs in the checklist are in either the
	// Failed or the Succeeded state. In other words, at least one component of
	// the checklist has candidate pairs that are all in the Failed state, which
	// means the component has failed, which means the checklist has failed.
	ChecklistFailed
)

// ComputePriorities computes priorities for all pairs based on agent role.
//
// The role determines whether local candidate is from controlling or from
// controlled agent.
func (c *Checklist) ComputePriorities(role Role) {
	for i := range c.Pairs {
		var (
			controlling = c.Pairs[i].Local.Priority
			controlled  = c.Pairs[i].Remote.Priority
		)
		if role == Controlled {
			controlling, controlled = controlled, controlling
		}
		c.Pairs[i].Priority = PairPriority(controlling, controlled)
	}
}

// Order is ordering pairs by priority descending.
// First element will have highest priority.
func (c *Checklist) Order() { sort.Sort(c.Pairs) }

// Prune removes redundant candidates.
//
// Two candidate pairs are redundant if their local candidates have the same
// base and their remote candidates are identical
func (c *Checklist) Prune() {
	// Pruning algorithm is not optimal but should work for small numbers,
	// where len(c.Pairs) ~ 100.
	result := make(Pairs, 0, len(c.Pairs))
Loop:
	for i := range c.Pairs {
		base := c.Pairs[i].Local.Base
		for j := range result {
			// Check if local candidates have the same base.
			if !result[j].Local.Base.Equal(base) {
				continue
			}
			// Check if remote candidates are identical.
			if !result[j].Remote.Equal(c.Pairs[i].Remote) {
				continue
			}
			// Pair is redundant, skipping.
			continue Loop
		}
		result = append(result, c.Pairs[i])
	}
	c.Pairs = result
}

// Limit ensures maximum length of pairs, removing the pairs with least priority
// if needed.
func (c *Checklist) Limit(max int) {
	if len(c.Pairs) <= max {
		return
	}
	c.Pairs = c.Pairs[:max]
}

// Len returns pairs count.
func (c *Checklist) Len() int {
	return len(c.Pairs)
}
