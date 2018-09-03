package server

import (
	"sync"
	"sync/atomic"
)

type Updater struct {
	v         atomic.Value
	mux       sync.RWMutex
	listeners []*Server
}

func (u *Updater) Get() Options {
	return u.v.Load().(Options)
}

func (u *Updater) Set(o Options) {
	u.v.Store(o)
	u.mux.RLock()
	for _, s := range u.listeners {
		s.setOptions(o)
	}
	u.mux.RUnlock()
}

func (u *Updater) Subscribe(s *Server) {
	u.mux.Lock()
	u.listeners = append(u.listeners, s)
	u.mux.Unlock()
}

func NewUpdater(o Options) *Updater {
	u := &Updater{}
	u.v.Store(o)
	return u
}
