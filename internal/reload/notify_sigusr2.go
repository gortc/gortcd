//+build !windows

package reload

import (
	"os"
	"os/signal"
	"syscall"
)

func (n *Notifier) subscribe() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGUSR2)
	go func() {
		n.log.Info("subscribed to SIGUSR2")
		for range c {
			n.Notify()
		}
	}()
}
