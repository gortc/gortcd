package reload

type Notifier struct {
	C chan struct{}
}

func NewNotifier() Notifier {
	n := Notifier{}
	n.subscribe()
	return n
}
