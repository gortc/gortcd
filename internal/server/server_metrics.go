package server

import "github.com/prometheus/client_golang/prometheus"

type noopMetrics struct{}

func (noopMetrics) incSTUNMessages() {}

type promMetrics struct {
	stunMessages prometheus.Counter
}

func newPromMetrics(labels prometheus.Labels) *promMetrics {
	p := &promMetrics{
		stunMessages: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "gortcd_stun_messages_count",
			Help:        "gortcd received STUN messages count excluding filtered by rules",
			ConstLabels: labels,
		}),
	}
	return p
}

func (m *promMetrics) Describe(d chan<- *prometheus.Desc) {
	d <- m.stunMessages.Desc()
}

func (m *promMetrics) Collect(c chan<- prometheus.Metric) {
	m.stunMessages.Collect(c)
}

func (m *promMetrics) incSTUNMessages() { m.stunMessages.Inc() }
