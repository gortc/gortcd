package manage

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type notifierFunc func()

func (f notifierFunc) Notify() { f() }

type errWriter struct{}

func (errWriter) Write(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

func TestManager_ErrorLogging(t *testing.T) {
	notifier := notifierFunc(func() {})
	core, logs := observer.New(zapcore.WarnLevel)
	m := NewManager(zap.New(core), notifier)
	m.fprintln(errWriter{}, "test")
	if logs.Len() != 1 {
		t.Error("unexpected log entry count")
	}
}

func TestManager_ServeHTTP(t *testing.T) {
	notified := false
	notifier := notifierFunc(func() {
		notified = true
	})
	s := httptest.NewServer(NewManager(zap.NewNop(), notifier))
	defer s.Close()
	c := s.Client()
	res, err := c.Get("http://" + s.Listener.Addr().String() + "/reload")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Error("bad status")
	}
	if !notified {
		t.Error("not notified")
	}
	res, err = c.Get("http://" + s.Listener.Addr().String() + "/random")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusNotFound {
		t.Error("bad status")
	}
}
