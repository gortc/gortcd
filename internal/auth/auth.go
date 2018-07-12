// Package auth implements authentication for gortcd.
package auth

import (
	"errors"
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

func (s *Static) Auth(m *stun.Message) (stun.MessageIntegrity, error) {
	var (
		username stun.Username
		err      error
	)
	// Getting username attr directly to remove unneeded allocs.
	if username, err = m.Get(stun.AttrUsername); err != nil {
		return nil, err
	}
	s.mux.RLock()
	i := s.credentials[string(username)]
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
