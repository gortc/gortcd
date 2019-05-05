package server

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestPromMetrics(t *testing.T) {
	pm := newPromMetrics(prometheus.Labels{"foo": "bar"})
	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(pm); err != nil {
		t.Error(err)
	}
	for i := 0; i < 10; i++ {
		pm.incSTUNMessages()
	}
	if _, err := reg.Gather(); err != nil {
		t.Error(err)
	}
}
