package server

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestWorkerPoolStartStopSerial(t *testing.T) {
	testWorkerPoolStartStop(t)
}

func TestWorkerPoolStartStopConcurrent(t *testing.T) {
	concurrency := 10
	ch := make(chan struct{}, concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			testWorkerPoolStartStop(t)
			ch <- struct{}{}
		}()
	}
	for i := 0; i < concurrency; i++ {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("timeout")
		}
	}
}

func testWorkerPoolStartStop(t *testing.T) {
	t.Helper()
	wp := &workerPool{
		WorkerFunc:      func(c *context) error { return nil },
		MaxWorkersCount: 10,
		Logger:          zap.NewNop(),
	}
	for i := 0; i < 10; i++ {
		wp.Start()
		wp.Stop()
	}
}
