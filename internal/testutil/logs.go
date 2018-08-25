package testutil

import (
	"testing"

	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// EnsureNoErrors calls the t.Error if there are any ErrorLevel entries in logs.
func EnsureNoErrors(t *testing.T, logs *observer.ObservedLogs) {
	t.Helper()
	for _, e := range logs.TakeAll() {
		if e.Level == zapcore.ErrorLevel {
			t.Error(e.Message)
		}
	}
}
