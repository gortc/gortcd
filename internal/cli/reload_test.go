package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/reload":
			if _, err := fmt.Fprintln(w, "Reloaded"); err != nil {
				t.Error(err)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	v := getViper()
	v.Set("api.addr", server.Listener.Addr())
	flags := getReloadCmd(v).Flags()
	_ = flags.Set("silent", "false")
	buf := new(bytes.Buffer)
	execReload(v, flags, buf)
	if s := buf.String(); strings.TrimSpace(s) != "OK - Reloaded" {
		t.Errorf("unexpected output: %s", s)
	}
}
