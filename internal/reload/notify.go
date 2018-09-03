package reload

// Notifier implements config reload request notification
type Notifier struct {
	C chan struct{}
}

// NewNotifier initializes and returns new notifier.
func NewNotifier() Notifier {
	n := Notifier{}
	n.subscribe()
	return n
}
