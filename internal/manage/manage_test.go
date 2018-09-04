package manage

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

type notifierFunc func()

func (f notifierFunc) Notify() {
	f()
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
