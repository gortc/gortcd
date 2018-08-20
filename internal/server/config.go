package server

import (
	"sync"
	"time"

	"github.com/gortc/stun"
)

type config struct {
	lock            sync.RWMutex
	maxLifetime     time.Duration
	defaultLifetime time.Duration
	workers         int
	authForSTUN     bool
	software        stun.Software
}

func (c *config) setAuthForSTUN(v bool) {
	c.lock.Lock()
	c.authForSTUN = v
	c.lock.Unlock()
}

func newConfig(options Options) *config {
	return &config{
		maxLifetime:     time.Hour,
		defaultLifetime: time.Minute,
		workers:         options.Workers,
		authForSTUN:     options.AuthForSTUN,
		software:        stun.NewSoftware(options.Software),
	}
}

func (c *config) DefaultLifetime() time.Duration {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.defaultLifetime
}

func (c *config) MaxLifetime() time.Duration {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.maxLifetime
}

func (c *config) Workers() int {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.workers
}

func (c *config) RequireAuthForSTUN() bool {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.authForSTUN
}

func (c *config) AppendSoftware(s stun.Software) stun.Software {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return append(s, c.software...)
}
