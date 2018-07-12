package auth

import (
	"testing"

	"github.com/gortc/stun"
)

func TestStatic_Auth(t *testing.T) {
	var (
		s = NewStatic([]StaticCredential{
			{Username: "username", Realm: "realm", Password: "password"},
		})
		i = stun.NewLongTermIntegrity("username", "realm", "password")
		u = stun.NewUsername("username")
	)
	for _, tc := range []struct {
		name string
		m    *stun.Message
		ok   bool
	}{
		{
			name: "positive",
			m:    stun.MustBuild(stun.BindingRequest, u, i),
			ok:   true,
		},
		{
			name: "negative",
			m: stun.MustBuild(stun.BindingRequest, u,
				stun.NewLongTermIntegrity("username", "realm", "password2"),
			),
			ok: false,
		},
		{
			name: "bad username",
			m:    stun.MustBuild(stun.BindingRequest, stun.NewUsername("user"), i),
			ok:   false,
		},
		{
			name: "no username",
			m:    stun.MustBuild(stun.BindingRequest, i),
			ok:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotI, err := s.Auth(tc.m)
			if !tc.ok {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Error(err)
			}
			r := stun.MustBuild(tc.m, u, gotI)
			if _, err = s.Auth(r); err != nil {
				t.Error(err)
			}
		})
	}
}
