package auth

import (
	"testing"

	"gortc.io/stun"

	"gortc.io/gortcd/internal/testutil"
)

func TestStatic_Auth(t *testing.T) {
	var (
		s = NewStatic([]StaticCredential{
			{Username: "username", Realm: "realm", Password: "password"},
		})
		i = stun.NewLongTermIntegrity("username", "realm", "password")
		u = stun.NewUsername("username")
		r = stun.NewRealm("realm")
	)
	t.Run("ZeroAlloc", func(t *testing.T) {
		m := stun.MustBuild(stun.BindingRequest, u, r, i)
		testutil.ShouldNotAllocate(t, func() {
			if _, err := s.Auth(m); err != nil {
				t.Fatal(err)
			}
		})
	})
	for _, tc := range []struct {
		name string
		m    *stun.Message
		ok   bool
	}{
		{
			name: "positive",
			m:    stun.MustBuild(stun.BindingRequest, u, r, i),
			ok:   true,
		},
		{
			name: "negative",
			m: stun.MustBuild(stun.BindingRequest, u, r,
				stun.NewLongTermIntegrity("username", "realm", "password2"),
			),
			ok: false,
		},
		{
			name: "bad username",
			m:    stun.MustBuild(stun.BindingRequest, stun.NewUsername("user"), r, i),
			ok:   false,
		},
		{
			name: "bad realm",
			m:    stun.MustBuild(stun.BindingRequest, u, stun.NewRealm("realm1"), i),
			ok:   false,
		},
		{
			name: "no username",
			m:    stun.MustBuild(stun.BindingRequest, r, i),
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
			r := stun.MustBuild(tc.m, u, r, gotI)
			if _, err = s.Auth(r); err != nil {
				t.Error(err)
			}
		})
	}
}

func BenchmarkStatic_Auth(b *testing.B) {
	var (
		s = NewStatic([]StaticCredential{
			{Username: "username", Realm: "realm", Password: "password"},
		})
		i = stun.NewLongTermIntegrity("username", "realm", "password")
		u = stun.NewUsername("username")
		r = stun.NewRealm("realm")
		m = stun.MustBuild(stun.BindingRequest, u, r, i)
	)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := s.Auth(m)
		if err != nil {
			b.Fatal(err)
		}
	}
}
