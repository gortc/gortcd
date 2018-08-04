package server

import (
	"sync"
	"time"
)

type config struct {
	lock            sync.RWMutex
	maxLifetime     time.Duration
	defaultLifetime time.Duration
	workers         int
	authForSTUN     bool
}

func newConfig(options Options) *config {
	return &config{
		maxLifetime:     time.Hour,
		defaultLifetime: time.Minute,
		workers:         options.Workers,
		authForSTUN:     options.AuthForSTUN,
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
