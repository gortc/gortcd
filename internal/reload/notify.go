package reload

import "go.uber.org/zap"

// Notifier implements config reload request notification
type Notifier struct {
	log *zap.Logger
	C   chan struct{}
}

// Notify about options reload request.
func (n *Notifier) Notify() {
	n.log.Info("notify")
	n.C <- struct{}{}
}

// NewNotifier initializes and returns new notifier.
func NewNotifier(l *zap.Logger) *Notifier {
	n := &Notifier{log: l, C: make(chan struct{}, 1)}
	n.subscribe()
	return n
}
