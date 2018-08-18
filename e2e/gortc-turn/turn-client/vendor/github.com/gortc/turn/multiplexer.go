package turn

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/gortc/stun"
)

// multiplexer de-multiplexes STUN, TURN and application data
// from one connection into separate ones.
type multiplexer struct {
	log      *zap.Logger
	capacity int
	conn     net.Conn

	stunL, stunR net.Conn
	turnL, turnR net.Conn
	dataL, dataR net.Conn
}

type bypassWriter struct {
	reader net.Conn
	writer net.Conn
}

func (w bypassWriter) Close() error {
	rErr := w.reader.Close()
	wErr := w.writer.Close()
	if rErr == nil && wErr == nil {
		return nil
	}
	return fmt.Errorf("reader: %v, writer: %v", rErr, wErr)
}

func (w bypassWriter) LocalAddr() net.Addr {
	return w.writer.LocalAddr()
}

func (w bypassWriter) Read(b []byte) (n int, err error) {
	return w.reader.Read(b)
}

func (w bypassWriter) RemoteAddr() net.Addr {
	return w.writer.RemoteAddr()
}

func (w bypassWriter) SetDeadline(t time.Time) error {
	if err := w.writer.SetDeadline(t); err != nil {
		return err
	}
	return w.reader.SetDeadline(t)
}

func (w bypassWriter) SetReadDeadline(t time.Time) error {
	return w.reader.SetReadDeadline(t)
}

func (w bypassWriter) SetWriteDeadline(t time.Time) error {
	return w.writer.SetWriteDeadline(t)
}

func (w bypassWriter) Write(b []byte) (n int, err error) {
	return w.writer.Write(b)
}

func newMultiplexer(conn net.Conn, log *zap.Logger) *multiplexer {
	m := &multiplexer{conn: conn, capacity: 1500, log: log}
	m.stunL, m.stunR = net.Pipe()
	m.turnL, m.turnR = net.Pipe()
	m.dataL, m.dataR = net.Pipe()
	go m.readUntilClosed()
	return m
}

func (m *multiplexer) discardData() {
	_, err := io.Copy(ioutil.Discard, m.dataL)
	if err != nil {
		m.log.Error("discard error", zap.Error(err))
	}
}

func (m *multiplexer) close() {
	if closeErr := m.turnR.Close(); closeErr != nil {
		m.log.Error("failed to close turnR", zap.Error(closeErr))
	}
	if closeErr := m.stunR.Close(); closeErr != nil {
		m.log.Error("failed to close stunR", zap.Error(closeErr))
	}
	if closeErr := m.dataR.Close(); closeErr != nil {
		m.log.Error("failed to close dataR", zap.Error(closeErr))
	}
}

func stunLog(ce *zapcore.CheckedEntry, data []byte) {
	m := &stun.Message{
		Raw: data,
	}
	if err := m.Decode(); err == nil {
		ce.Write(zap.Stringer("msg", m))
	}
}

func (m *multiplexer) readUntilClosed() {
	buf := make([]byte, m.capacity)
	for {
		n, err := m.conn.Read(buf)
		if ce := m.log.Check(zap.DebugLevel, "read"); ce != nil {
			ce.Write(zap.Error(err), zap.Int("n", n))
		}
		if err != nil {
			// End of cycle.
			// TODO: Handle timeouts and temporary errors.
			m.log.Error("failed to read", zap.Error(err))
			m.close()
			break
		}
		data := buf[:n]
		conn := m.dataR
		switch {
		case stun.IsMessage(data):
			m.log.Debug("got STUN data")
			if ce := m.log.Check(zap.DebugLevel, "stun message"); ce != nil {
				stunLog(ce, data)
			}
			conn = m.stunR
		case IsChannelData(data):
			m.log.Debug("got TURN data")
			conn = m.turnR
		default:
			m.log.Debug("got APP data")
		}
		_, err = conn.Write(data)
		if err != nil {
			m.log.Warn("failed to write", zap.Error(err))
		}
	}
}
