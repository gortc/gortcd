// Package auth implements authentication for gortcd.
package auth

import (
	"errors"
	"sync"

	"gortc.io/stun"
)

// StaticCredential wraps plain Username, Password and Realm,
// representing a long-term credential.
type StaticCredential struct {
	Username string
	Password string
	Realm    string
	Key      []byte
}

type staticKey struct {
	username string
	realm    string
}

// Static implements authentication with pre-defined static list
// of long-term credentials.
type Static struct {
	mux         sync.RWMutex
	credentials map[staticKey]stun.MessageIntegrity
}

// Auth perform authentication of m and returns integrity that can
// be used to construct response to m.
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
	i := s.credentials[staticKey{username: string(username), realm: string(realm)}]
	s.mux.RUnlock()
	if i == nil {
		return nil, errors.New("user not found")
	}
	return i, i.Check(m)
}

// NewStatic initializes new static authenticator with list of long-term
// credentials.
func NewStatic(credentials []StaticCredential) *Static {
	s := &Static{
		credentials: make(map[staticKey]stun.MessageIntegrity, len(credentials)),
	}
	for _, c := range credentials {
		k := staticKey{username: c.Username, realm: c.Realm}
		if len(c.Key) > 0 {
			s.credentials[k] = stun.MessageIntegrity(c.Key)
			continue
		}
		s.credentials[k] = stun.NewLongTermIntegrity(c.Username, c.Realm, c.Password)
	}
	return s
}
