package turn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/gortc/stun"
)

// Permission implements net.PacketConn.
type Permission struct {
	log          *zap.Logger
	mux          sync.RWMutex
	number       ChannelNumber
	peerAddr     PeerAddress
	peerL, peerR net.Conn
	client       *Client
	ctx          context.Context
	cancel       func()
	wg           sync.WaitGroup
	refreshRate  time.Duration
}

// Read data from peer.
func (p *Permission) Read(b []byte) (n int, err error) {
	return p.peerR.Read(b)
}

// Bound returns true if channel number is bound for current permission.
func (p *Permission) Bound() bool {
	p.mux.RLock()
	defer p.mux.RUnlock()
	return p.number.Valid()
}

// Binding returns current channel number or 0 if not bound.
func (p *Permission) Binding() ChannelNumber {
	p.mux.RLock()
	defer p.mux.RUnlock()
	return p.number
}

var (
	// ErrAlreadyBound means that selected permission already has bound channel number.
	ErrAlreadyBound = errors.New("channel already bound")
	// ErrNotBound means that selected permission already has no channel number.
	ErrNotBound = errors.New("channel is not bound")
)

func (p *Permission) refresh() error {
	return p.client.alloc.allocate(p.peerAddr)
}

func (p *Permission) startLoop(f func()) {
	p.wg.Add(1)
	go func() {
		ticker := time.NewTicker(p.refreshRate)
		defer p.wg.Done()
		for {
			select {
			case <-ticker.C:
				f()
			case <-p.ctx.Done():
				return
			}
		}
	}()
}

func (p *Permission) startRefreshLoop() {
	p.startLoop(func() {
		if err := p.refresh(); err != nil {
			p.log.Error("failed to refresh permission", zap.Error(err))
		}
		p.log.Debug("permission refreshed")
	})
}

// refreshBind performs rebinding of a channel.
func (p *Permission) refreshBind() error {
	p.mux.Lock()
	defer p.mux.Unlock()
	if p.number == 0 {
		return ErrNotBound
	}
	if err := p.bind(p.number); err != nil {
		return err
	}
	p.log.Debug("binding refreshed")
	return nil
}

func (p *Permission) bind(n ChannelNumber) error {
	// Starting transaction.
	a := p.client.alloc
	res := stun.New()
	req := stun.New()
	req.TransactionID = stun.NewTransactionID()
	req.Type = stun.NewType(stun.MethodChannelBind, stun.ClassRequest)
	req.WriteHeader()
	setters := make([]stun.Setter, 0, 10)
	setters = append(setters, &p.peerAddr, n)
	if len(a.integrity) > 0 {
		// Applying auth.
		setters = append(setters,
			a.nonce, a.client.username, a.client.realm, a.integrity,
		)
	}
	setters = append(setters, stun.Fingerprint)
	for _, s := range setters {
		if setErr := s.AddTo(req); setErr != nil {
			return setErr
		}
	}
	if doErr := p.client.do(req, res); doErr != nil {
		return doErr
	}
	if res.Type != stun.NewType(stun.MethodChannelBind, stun.ClassSuccessResponse) {
		return fmt.Errorf("unexpected response type %s", res.Type)
	}
	// Success.
	return nil
}

// Bind performs binding transaction, allocating channel binding for
// the permission.
func (p *Permission) Bind() error {
	p.mux.Lock()
	defer p.mux.Unlock()
	if p.number != 0 {
		return ErrAlreadyBound
	}
	a := p.client.alloc
	a.minBound++
	n := a.minBound
	if err := p.bind(n); err != nil {
		return err
	}
	p.number = n
	if p.refreshRate > 0 {
		p.startLoop(func() {
			if err := p.refreshBind(); err != nil {
				p.log.Error("failed to refresh bind", zap.Error(err))
			}
		})
	}
	return nil
}

// Write sends buffer to peer.
//
// If permission is bound, the ChannelData message will be used.
func (p *Permission) Write(b []byte) (n int, err error) {
	if n := p.Binding(); n.Valid() {
		if ce := p.log.Check(zap.DebugLevel, "using channel data to write"); ce != nil {
			ce.Write()
		}
		return p.client.sendChan(b, n)
	}
	if ce := p.log.Check(zap.DebugLevel, "using STUN to write"); ce != nil {
		ce.Write()
	}
	return p.client.sendData(b, &p.peerAddr)
}

// Close stops all refreshing loops for permission and removes it from
// allocation.
func (p *Permission) Close() error {
	cErr := p.peerR.Close()
	p.mux.Lock()
	cancel := p.cancel
	p.mux.Unlock()
	cancel()
	p.wg.Wait()
	p.client.alloc.removePermission(p)
	return cErr
}

// LocalAddr is relayed address from TURN server.
func (p *Permission) LocalAddr() net.Addr {
	return Addr(p.client.alloc.relayed)
}

// RemoteAddr is peer address.
func (p *Permission) RemoteAddr() net.Addr {
	return Addr(p.peerAddr)
}

// SetDeadline implements net.Conn.
func (p *Permission) SetDeadline(t time.Time) error {
	return p.peerR.SetDeadline(t)
}

// SetReadDeadline implements net.Conn.
func (p *Permission) SetReadDeadline(t time.Time) error {
	return p.peerR.SetReadDeadline(t)
}

// ErrNotImplemented means that functionality is not currently implemented,
// but it will be (eventually).
var ErrNotImplemented = errors.New("functionality not implemented")

// SetWriteDeadline implements net.Conn.
func (p *Permission) SetWriteDeadline(t time.Time) error {
	return ErrNotImplemented
}
