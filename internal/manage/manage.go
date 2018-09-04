// Package manage implements management of server.
package manage

import (
	"fmt"
	"net/http"
)

// Notifier wraps notify method.
type Notifier interface {
	Notify()
}

// Manager handles http management endpoints.
type Manager struct {
	notifier Notifier
}

// ServeHTTP implements http.Handler.
func (m Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/reload":
		w.WriteHeader(http.StatusOK)
		m.notifier.Notify()
		fmt.Fprintln(w, "server will be reloaded soon")
	default:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, "management endpoint not found")
	}
}

// NewManager initializes and returns Manager.
func NewManager(n Notifier) Manager {
	return Manager{
		notifier: n,
	}
}
