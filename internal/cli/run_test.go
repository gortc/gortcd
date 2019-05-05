package cli

import (
	"io/ioutil"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/gortc/gortcd/internal/server"
)

func TestParseFiltering(t *testing.T) {
	defer viper.Reset()
	viper.Set("filter.key.rules", []map[string]string{
		{"net": "10.0.0.0/24", "action": "allow"},
		{"net": "20.0.0.0/24", "action": "deny"},
		{"net": "30.0.0.0/24", "action": "pass"},
	})
	viper.Set("filter.key.action", "drop")
	rules, err := parseFilteringRules(zap.NewNop(), "key")
	if err != nil {
		t.Error(err)
	}
	if rules == nil {
		t.Error(err)
	}
}

func TestConfig(t *testing.T) {
	defer viper.Reset()
	t.Run("Default", func(t *testing.T) {
		initConfig()
		logCfg, logErr := getZapConfig()
		if logErr != nil {
			t.Fatal(logErr)
		}
		l, buildErr := logCfg.Build()
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		opt := server.Options{}
		if err := parseOptions(l, &opt); err != nil {
			t.Fatal(err)
		}
	})
}

func TestParseStaticCredentials(t *testing.T) {
	defer viper.Reset()
	viper.Set("auth.static", []map[string]string{
		{"username": "user", "password": "secret"},
		{"username": "foo", "key": "0x0F"},
	})
	creds := parseStaticCredentials(zap.NewNop(), "realm")
	if len(creds) == 0 {
		t.Fatal("failed to parse")
	}
	if creds[0].Realm != "realm" {
		t.Error("bad realm")
	}
	if creds[0].Password != "secret" {
		t.Error("bad password")
	}
	if creds[0].Username != "user" {
		t.Error("bad username")
	}
	if creds[1].Key[0] != 0x0F {
		t.Error("bad key")
	}
}

func TestSnap(t *testing.T) {
	name, err := ioutil.TempDir("", "gortcd_snap")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.RemoveAll(name)
	}()

	defer func(v string) {
		_ = os.Setenv("SNAP_USER_DATA", v)
	}(os.Getenv("SNAP_USER_DATA"))

	if err = os.Setenv("SNAP_USER_DATA", name); err != nil {
		t.Fatal(err)
	}
	initConfigSnap()
}

func TestGetListeners(t *testing.T) {
	defer viper.Reset()
	tf, err := ioutil.TempFile("", "gortcd-temp-cfg.*.yml")
	if err != nil {
		t.Fatal(err)
	}
	tfName := tf.Name()
	if _, err = tf.WriteString(defaultConfigFileContent); err != nil {
		t.Fatal(err)
	}
	if err = tf.Close(); err != nil {
		t.Fatal(err)
	}

	defer func() { _ = os.Remove(tfName) }()
	defer func(oldCfgFile string) { cfgFile = oldCfgFile }(cfgFile)
	cfgFile = tfName

	initConfig()

	viper.SetDefault("server.prometheus.addr", "127.0.0.0:0")
	viper.SetDefault("server.pprof", "127.0.0.0:0")
	viper.SetDefault("api.addr", "127.0.0.0:0")

	core, logs := observer.New(zap.DebugLevel)
	l := zap.New(core)
	listeners := getListeners(l)
	if len(listeners) == 0 {
		t.Error("no listeners")
	}
	checked := map[string]bool{
		"config file": false,
		"api":         false,
	}
	for _, e := range logs.All() {
		t.Logf("%s %v", e.Message, e.Context)
		switch e.Message {
		case "config file used":
			cfgPath := ""
			for _, field := range e.Context {
				if field.Key == "path" {
					cfgPath = field.String
				}
			}
			if cfgPath != tfName {
				t.Error("bad cfg file")
			}
			checked["config file"] = true
		case "api listening":
			apiURL := ""
			for _, field := range e.Context {
				if field.Key == "addr" {
					apiURL = "http://" + field.String + "/reload"
				}
			}
			var resp *http.Response
			for i := 0; i < 7; i++ {
				resp, err = http.Get(apiURL)
				if err == nil {
					break
				}
				time.Sleep(time.Millisecond * 20)
			}
			if err != nil {
				t.Fatal(err)
			}
			switch resp.StatusCode {
			case http.StatusOK:
				t.Log("reloaded")
			default:
				t.Error("bad status code")
			}
			checked["api"] = true
		}
	}
	for k, v := range checked {
		if !v {
			t.Errorf("%s is not checked", k)
		}
	}
}
