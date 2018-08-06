package ice

import "github.com/gortc/stun"

// Priority represents PRIORITY attribute.
type Priority uint32

const prioritySize = 4 // 32 bit

// AddTo adds PRIORITY attribute to message.
func (p Priority) AddTo(m *stun.Message) error {
	v := make([]byte, prioritySize)
	bin.PutUint32(v, uint32(p))
	m.Add(stun.AttrPriority, v)
	return nil
}

// GetFrom decodes PRIORITY attribute from message.
func (p *Priority) GetFrom(m *stun.Message) error {
	v, err := m.Get(stun.AttrPriority)
	if err != nil {
		return err
	}
	if err = stun.CheckSize(stun.AttrPriority, len(v), prioritySize); err != nil {
		return err
	}
	*p = Priority(bin.Uint32(v))
	return nil
}
