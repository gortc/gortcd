// Package manage implements management of server.
package manage

import (
	"fmt"
	"io"
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

func (m Manager) fprintln(w io.Writer, a ...interface{}) {
	if _, err := fmt.Fprintln(w, a...); err != nil {
		m.l.Warn("failed to write", zap.Error(err))
	}
}

// ServeHTTP implements http.Handler.
func (m Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/reload":
		m.l.Info("got reload request")
		w.WriteHeader(http.StatusOK)
		m.notifier.Notify()
		m.fprintln(w, "server will be reloaded soon")
	default:
		w.WriteHeader(http.StatusNotFound)
		m.fprintln(w, "management endpoint not found")
	}
}

// NewManager initializes and returns Manager.
func NewManager(l *zap.Logger, n Notifier) Manager { return Manager{l: l, notifier: n} }
