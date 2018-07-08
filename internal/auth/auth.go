// Package auth implements authentication for gortcd.
package auth

import (
	"sync"

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

func (s *Static) Auth(r *Request) (stun.MessageIntegrity, error) {
	s.mux.RLock()
	i := s.credentials[r.Username.String()]
	s.mux.RUnlock()
	return i, nil
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
