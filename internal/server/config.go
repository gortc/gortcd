package server

import (
	"time"

	"gortc.io/stun"

	"gortc.io/gortcd/internal/filter"
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
	metrics         metrics
	metricsEnabled  bool
}

var metricsNoop = noopMetrics{}

func (s *Server) newConfig(options Options) config {
	cfg := config{
		maxLifetime:     time.Hour,
		defaultLifetime: time.Minute,
		workers:         options.Workers,
		authForSTUN:     options.AuthForSTUN,
		software:        stun.NewSoftware(options.Software),
		clientFilter:    options.ClientRule,
		peerFilter:      options.PeerRule,
		realm:           stun.NewRealm(options.Realm),
		debugCollect:    options.DebugCollect,
		metrics:         metricsNoop,
	}
	if options.MetricsEnabled {
		cfg.metrics = s.promMetrics
	}
	return cfg
}

type metrics interface {
	incSTUNMessages()
}
