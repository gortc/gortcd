// Package auth implements authentication for gortcd.
package auth

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/gortc/stun"
)

type StaticCredential struct {
	Username string
	Password string
	Realm    string
}

type Static struct {
	mux         sync.RWMutex
	credentials map[string]stun.MessageIntegrity
}

type Request struct {
	Username stun.Username
	Realm    stun.Realm
}

type Response struct {
	Integrity stun.MessageIntegrity
}

// b2s converts byte slice to a string without memory allocation.
// See https://groups.google.com/forum/#!msg/Golang-Nuts/ENgbUzYvCuU/90yGx7GUAgAJ .
//
// Note it may break if string and/or slice header will change
// in the future go versions.
func b2s(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

func (s *Static) Auth(m *stun.Message) (stun.MessageIntegrity, error) {
	var (
		username stun.Username
	)
	if err := username.GetFrom(m); err != nil {
		return nil, err
	}
	s.mux.RLock()
	i := s.credentials[b2s(username)]
	s.mux.RUnlock()
	if i == nil {
		return nil, errors.New("user not found")
	}
	return i, i.Check(m)
}

func NewStatic(credentials []StaticCredential) *Static {
	s := &Static{
		credentials: make(map[string]stun.MessageIntegrity, len(credentials)),
	}
	for _, c := range credentials {
		s.credentials[c.Username] = stun.NewLongTermIntegrity(
			c.Username, c.Realm, c.Password,
		)
	}
	return s
}
