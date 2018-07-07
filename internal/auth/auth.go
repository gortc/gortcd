// Package auth implements authentication for gortcd.
package auth

import (
	"github.com/gortc/stun"
	"sync"
)

type StaticCredential struct {
	Username string
	Password string
	Realm    string
}

type Static struct {
	mux         sync.RWMutex
	credentials map[string]StaticCredential
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
	c := s.credentials[r.Username.String()]
	s.mux.RUnlock()
	i := stun.NewLongTermIntegrity(c.Username, c.Realm, c.Password)
	return i, nil
}

func NewStatic(credentials []StaticCredential) *Static {
	s := &Static{
		credentials: make(map[string]StaticCredential, len(credentials)),
	}
	for _, c := range credentials {
		s.credentials[c.Username] = c
	}
	return s
}
