package server

import (
	"sync"
	"sync/atomic"
)

// Updater handles options update.
type Updater struct {
	v         atomic.Value
	mux       sync.RWMutex
	listeners []*Server
}

// Get returns current options.
func (u *Updater) Get() Options {
	return u.v.Load().(Options)
}

// Set stores new options and notifies all listeners.
func (u *Updater) Set(o Options) {
	u.v.Store(o)
	u.mux.RLock()
	for _, s := range u.listeners {
		s.setOptions(o)
	}
	u.mux.RUnlock()
}

// Subscribe adds server to listeners.
func (u *Updater) Subscribe(s *Server) {
	u.mux.Lock()
	u.listeners = append(u.listeners, s)
	u.mux.Unlock()
}

// NewUpdater initializes new updater from options.
func NewUpdater(o Options) *Updater {
	u := &Updater{}
	u.v.Store(o)
	return u
}
