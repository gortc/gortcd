package turn

import (
	"strconv"

	"github.com/gortc/stun"
)

// ChannelNumber represents CHANNEL-NUMBER attribute.
//
// The CHANNEL-NUMBER attribute contains the number of the channel.
//
// RFC 5766 Section 14.1
type ChannelNumber int // encoded as uint16

func (n ChannelNumber) String() string { return strconv.Itoa(int(n)) }

// 16 bits of uint + 16 bits of RFFU = 0.
const channelNumberSize = 4

// AddTo adds CHANNEL-NUMBER to message.
func (n ChannelNumber) AddTo(m *stun.Message) error {
	v := make([]byte, channelNumberSize)
	bin.PutUint16(v[:2], uint16(n))
	// v[2:4] are zeroes (RFFU = 0)
	m.Add(stun.AttrChannelNumber, v)
	return nil
}

// GetFrom decodes CHANNEL-NUMBER from message.
func (n *ChannelNumber) GetFrom(m *stun.Message) error {
	v, err := m.Get(stun.AttrChannelNumber)
	if err != nil {
		return err
	}
	if len(v) != channelNumberSize {
		return &BadAttrLength{
			Attr:     stun.AttrChannelNumber,
			Got:      len(v),
			Expected: channelNumberSize,
		}
	}
	_ = v[channelNumberSize-1] // asserting length
	*n = ChannelNumber(bin.Uint16(v[:2]))
	// v[2:4] is RFFU and equals to 0.
	return nil
}
