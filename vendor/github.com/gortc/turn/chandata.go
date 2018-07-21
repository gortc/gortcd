package turn

import (
	"bytes"
	"errors"
	"io"
)

// ChannelData represents the ChannelData Message.
//
// See RFC 5766 Section 11.4
type ChannelData struct {
	Data   []byte // can be subslice of Raw
	Length int    // ignored while encoding, len(Data) is used
	Number ChannelNumber
	Raw    []byte
}

// See https://tools.ietf.org/html/rfc5766#section-11:
//
// 0x4000 through 0x7FFF: These values are the allowed channel
// numbers (16,383 possible values).
const (
	maxChannelNumber = 0x7FFF
	minChannelNumber = 0x4000
)

// ErrInvalidChannelNumber means that channel number is not valid as by RFC 5766 Section 11.
var ErrInvalidChannelNumber = errors.New("channel number not in [0x4000, 0x7FFF]")

// isChannelNumberValid returns true if c complies to RFC 5766 Section 11.
func isChannelNumberValid(c ChannelNumber) bool {
	return c >= minChannelNumber && c <= maxChannelNumber
}

// Equal returns true if b == c.
func (c *ChannelData) Equal(b *ChannelData) bool {
	if c == nil && b == nil {
		return true
	}
	if c == nil || b == nil {
		return false
	}
	if c.Number != b.Number {
		return false
	}
	if len(c.Data) != len(b.Data) {
		return false
	}
	return bytes.Equal(c.Data, b.Data)
}

// grow ensures that internal buffer will fit v more bytes and
// increases it capacity if necessary.
func (c *ChannelData) grow(v int) {
	// Not performing any optimizations here
	// (e.g. preallocate len(buf) * 2 to reduce allocations)
	// because they are already done by []byte implementation.
	n := len(c.Raw) + v
	for cap(c.Raw) < n {
		c.Raw = append(c.Raw, 0)
	}
	c.Raw = c.Raw[:n]
}

// Reset resets ChannelData, data and underlying buffer length.
func (c *ChannelData) Reset() {
	c.Raw = c.Raw[:0]
	c.Length = 0
	c.Data = c.Data[:0]
}

// Encode encodes ChannelData Message to Raw.
func (c *ChannelData) Encode() {
	c.Raw = c.Raw[:0]
	c.WriteHeader()
	c.Raw = append(c.Raw, c.Data...)
}

// WriteHeader writes channel number and length.
func (c *ChannelData) WriteHeader() {
	if len(c.Raw) < channelDataHeaderSize {
		// Making WriteHeader call valid even when m.Raw
		// is nil or len(m.Raw) is less than needed for header.
		c.grow(channelDataHeaderSize)
	}
	// early bounds check to guarantee safety of writes below
	_ = c.Raw[:channelDataHeaderSize]
	bin.PutUint16(c.Raw[:channelNumberSize], uint16(c.Number))
	bin.PutUint16(c.Raw[channelNumberSize:channelDataHeaderSize],
		uint16(len(c.Data)),
	)
}

// ErrBadChannelDataLength means that channel data length is not equal
// to actual data length.
var ErrBadChannelDataLength = errors.New("channelData length != len(Data)")

// Decode decodes The ChannelData Message from Raw.
func (c *ChannelData) Decode() error {
	// Decoding message header.
	buf := c.Raw
	if len(buf) < channelDataHeaderSize {
		return io.ErrUnexpectedEOF
	}
	// Quick check for channel number.
	num := bin.Uint16(buf[0:channelNumberSize])
	c.Number = ChannelNumber(num)
	l := bin.Uint16(buf[channelNumberSize:channelDataHeaderSize])
	c.Data = buf[channelDataHeaderSize:]
	c.Length = int(l)
	if int(l) != len(buf[channelDataHeaderSize:]) {
		return ErrBadChannelDataLength
	}
	if !isChannelNumberValid(c.Number) {
		return ErrInvalidChannelNumber
	}
	return nil
}

const (
	channelDataLengthSize = channelNumberSize
	channelDataHeaderSize = channelNumberSize + channelDataLengthSize
)

// IsChannelData returns true if buf looks like the ChannelData Message.
func IsChannelData(buf []byte) bool {
	if len(buf) < channelDataHeaderSize {
		return false
	}
	// Quick check for channel number.
	num := bin.Uint16(buf[0:channelNumberSize])
	if !isChannelNumberValid(ChannelNumber(num)) {
		return false
	}
	// Check that length is valid.
	l := bin.Uint16(buf[channelNumberSize:channelDataHeaderSize])
	return int(l) == len(buf[channelDataHeaderSize:])
}
