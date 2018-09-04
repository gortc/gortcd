package reload

func (n *Notifier) subscribe() {
	// Not implemented.
	n.log.Warn("signal-based notify not supported on Windows")
}
