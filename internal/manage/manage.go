// Package manage implements management of server.
package manage

import (
	"fmt"
	"net/http"

	"go.uber.org/zap"
)

// Notifier wraps notify method.
type Notifier interface {
	Notify()
}

// Manager handles http management endpoints.
type Manager struct {
	notifier Notifier
	l        *zap.Logger
}

// ServeHTTP implements http.Handler.
func (m Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/reload":
		m.l.Info("got reload request")
		w.WriteHeader(http.StatusOK)
		m.notifier.Notify()
		if _, err := fmt.Fprintln(w, "server will be reloaded soon"); err != nil {
			m.l.Warn("failed to write", zap.Error(err))
		}
	default:
		w.WriteHeader(http.StatusNotFound)
		if _, err := fmt.Fprintln(w, "management endpoint not found"); err != nil {
			m.l.Warn("failed to write", zap.Error(err))
		}
	}
}

// NewManager initializes and returns Manager.
func NewManager(l *zap.Logger, n Notifier) Manager {
	return Manager{
		l:        l,
		notifier: n,
	}
}
