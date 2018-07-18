package server

import (
	"sync"
	"time"
)

type config struct {
	lock            sync.RWMutex
	maxLifetime     time.Duration
	defaultLifetime time.Duration
}

func newConfig(options Options) *config {
	return &config{
		maxLifetime:     time.Hour,
		defaultLifetime: time.Minute,
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
