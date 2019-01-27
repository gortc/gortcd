package server

import (
	"time"

	"github.com/gortc/gortcd/internal/filter"
	"github.com/gortc/stun"
)

type config struct {
	realm           stun.Realm
	maxLifetime     time.Duration
	defaultLifetime time.Duration
	workers         int
	authForSTUN     bool
	debugCollect    bool
	software        stun.Software
	peerFilter      filter.Rule
	clientFilter    filter.Rule
}

func newConfig(options Options) config {
	return config{
		maxLifetime:     time.Hour,
		defaultLifetime: time.Minute,
		workers:         options.Workers,
		authForSTUN:     options.AuthForSTUN,
		software:        stun.NewSoftware(options.Software),
		clientFilter:    options.ClientRule,
		peerFilter:      options.PeerRule,
		realm:           stun.NewRealm(options.Realm),
		debugCollect:    options.DebugCollect,
	}
}
