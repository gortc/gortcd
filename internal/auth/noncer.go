package auth

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/gortc/gortcd/internal/allocator"
	"github.com/gortc/stun"
)

// NewNonceAuth initializes new nonce manager.
//
// TODO: Run timer that removes old nonces
func NewNonceAuth(duration time.Duration) *NonceAuth {
	return &NonceAuth{
		nonces:   make([]nonce, 0, 100),
		duration: duration,
	}
}

type nonce struct {
	tuple      allocator.FiveTuple
	value      stun.Nonce
	validUntil time.Time
}

func (n nonce) valid(t time.Time) bool {
	if n.validUntil.IsZero() {
		return true
	}
	return n.validUntil.After(t)
}

// NonceAuth is nonce check and rotate implementation.
type NonceAuth struct {
	duration time.Duration
	mux      sync.Mutex
	nonces   []nonce
}

var (
	ErrStaleNonce = errors.New("stale nonce")
)

func newNonce() stun.Nonce {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	v := make([]byte, 24)
	return v[:hex.Encode(v, buf)]
}

// Check implements NonceManager.
func (n *NonceAuth) Check(tuple allocator.FiveTuple, value stun.Nonce, at time.Time) (stun.Nonce, error) {
	n.mux.Lock()
	defer n.mux.Unlock()
	for i := range n.nonces {
		if !n.nonces[i].tuple.Equal(tuple) {
			continue
		}
		// Found nonce.
		current := n.nonces[i]
		if current.valid(at) {
			// Current nonce is valid.
			if !bytes.Equal(current.value, value) {
				return current.value, ErrStaleNonce
			}
			return current.value, nil
		}
		// Rotating.
		current.value = newNonce()
		n.nonces[i] = current
		return current.value, ErrStaleNonce
	}
	current := nonce{
		tuple: tuple,
		value: newNonce(),
	}
	if n.duration != 0 {
		current.validUntil = at.Add(n.duration)
	}
	n.nonces = append(n.nonces, current)
	return current.value, ErrStaleNonce

}
