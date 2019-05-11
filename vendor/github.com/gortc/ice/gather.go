package ice

import (
	"github.com/gortc/ice/gather"
)

// Gather via DefaultGatherer.
func Gather() ([]gather.Addr, error) {
	return gather.DefaultGatherer.Gather()
}
