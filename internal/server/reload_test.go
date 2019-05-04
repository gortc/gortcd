package server

import "testing"

func TestNewUpdater(t *testing.T) {
	opt := Options{
		AuthForSTUN: true,
	}
	server, stop := newServer(t, opt)
	defer stop()
	u := NewUpdater(opt)
	u.Subscribe(server)
	opt = u.Get()
	if !opt.AuthForSTUN {
		t.Error("options mismatch")
	}
	if !server.config().authForSTUN {
		t.Error("options mismatch")
	}
	opt.AuthForSTUN = false
	u.Set(opt)
	optUpdated := u.Get()
	if optUpdated.AuthForSTUN {
		t.Error("options mismatch")
	}
	if server.config().authForSTUN {
		t.Error("options mismatch")
	}
}
