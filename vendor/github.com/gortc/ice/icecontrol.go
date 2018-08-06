package ice

import "github.com/gortc/stun"

// tieBreaker is common helper for ICE-{CONTROLLED,CONTROLLING}
// and represents the so-called tie-breaker number.
type tieBreaker uint64

const tieBreakerSize = 8 // 64 bit

// AddToAs adds tieBreaker value to m as t attribute.
func (a tieBreaker) AddToAs(m *stun.Message, t stun.AttrType) error {
	v := make([]byte, tieBreakerSize)
	bin.PutUint64(v, uint64(a))
	m.Add(t, v)
	return nil
}

// GetFromAs decodes tieBreaker value in message getting it as for t type.
func (a *tieBreaker) GetFromAs(m *stun.Message, t stun.AttrType) error {
	v, err := m.Get(t)
	if err != nil {
		return err
	}
	if err = stun.CheckSize(t, len(v), tieBreakerSize); err != nil {
		return err
	}
	*a = tieBreaker(bin.Uint64(v))
	return nil
}

// Controlled represents ICE-CONTROLLED attribute.
type Controlled uint64

// AddTo adds ICE-CONTROLLED to message.
func (c Controlled) AddTo(m *stun.Message) error {
	return tieBreaker(c).AddToAs(m, stun.AttrICEControlled)
}

// GetFrom decodes ICE-CONTROLLED from message.
func (c *Controlled) GetFrom(m *stun.Message) error {
	return (*tieBreaker)(c).GetFromAs(m, stun.AttrICEControlled)
}

// Controlling represents ICE-CONTROLLING attribute.
type Controlling uint64

// AddTo adds ICE-CONTROLLING to message.
func (c Controlling) AddTo(m *stun.Message) error {
	return tieBreaker(c).AddToAs(m, stun.AttrICEControlling)
}

// GetFrom decodes ICE-CONTROLLING from message.
func (c *Controlling) GetFrom(m *stun.Message) error {
	return (*tieBreaker)(c).GetFromAs(m, stun.AttrICEControlling)
}
