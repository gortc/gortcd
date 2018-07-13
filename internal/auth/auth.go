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

type staticKey struct {
	username string
	realm    string
}

type Static struct {
	mux         sync.RWMutex
	credentials map[staticKey]stun.MessageIntegrity
}

type Request struct {
	Username stun.Username
	Realm    stun.Realm
}

type Response struct {
	Integrity stun.MessageIntegrity
}

func (s *Static) Auth(m *stun.Message) (stun.MessageIntegrity, error) {
	username, err := m.Get(stun.AttrUsername)
	if err != nil {
		return nil, err
	}
	realm, err := m.Get(stun.AttrRealm)
	if err != nil {
		return nil, err
	}
	s.mux.RLock()
	i := s.credentials[staticKey{
		username: string(username),
		realm:    string(realm),
	}]
	s.mux.RUnlock()
	if i == nil {
		return nil, errors.New("user not found")
	}
	return i, i.Check(m)
}

func NewStatic(credentials []StaticCredential) *Static {
	s := &Static{
		credentials: make(map[staticKey]stun.MessageIntegrity, len(credentials)),
	}
	for _, c := range credentials {
		s.credentials[staticKey{
			username: c.Username,
			realm:    c.Realm,
		}] = stun.NewLongTermIntegrity(
			c.Username, c.Realm, c.Password,
		)
	}
	return s
}
