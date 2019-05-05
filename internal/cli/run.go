// Package cli implements command line interface for gortcd.
package cli

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/libp2p/go-reuseport"
	"github.com/mitchellh/go-homedir"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"

	"github.com/gortc/gortcd/internal/auth"
	"github.com/gortc/gortcd/internal/filter"
	"github.com/gortc/gortcd/internal/manage"
	"github.com/gortc/gortcd/internal/reload"
	"github.com/gortc/gortcd/internal/server"
	"github.com/gortc/ice"
	"github.com/gortc/stun"
)

// ListenUDPAndServe listens on laddr and process incoming packets.
func ListenUDPAndServe(serverNet, laddr string, u *server.Updater) error {
	var (
		c   net.PacketConn
		err error
	)
	opt := u.Get()
	if reuseport.Available() && opt.ReusePort {
		c, err = reuseport.ListenPacket(serverNet, laddr)
	} else {
		c, err = net.ListenPacket(serverNet, laddr)
	}
	if err != nil {
		return err
	}
	opt.Conn = c
	s, err := server.New(opt)
	if err != nil {
		return err
	}
	u.Subscribe(s)
	return s.Serve()
}

func normalize(address string) string {
	if address == "" {
		address = "0.0.0.0"
	}
	if !strings.Contains(address, ":") {
		address = fmt.Sprintf("%s:%d", address, stun.DefaultPort)
	}
	return address
}

type staticCredElem struct {
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Key      string `mapstructure:"key"`
	Realm    string `mapstructure:"realm"`
}

// getZapConfig decodes zap logging configuration from
// configuration file.
func getZapConfig(v *viper.Viper) (zap.Config, error) {
	// server.log
	type cfgWrapper struct {
		Server struct {
			Log zap.Config `yaml:"log"`
		} `yaml:"server"`
	}

	// Default logging configuration.
	d := zap.Config{
		DisableCaller:     true,
		DisableStacktrace: true,
		Level:             zap.NewAtomicLevel(),
		Development:       false,
		Sampling: &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		},
		Encoding: "json",
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "ts",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			MessageKey:     "msg",
			StacktraceKey:  "stacktrace",
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.EpochTimeEncoder,
			EncodeDuration: zapcore.SecondsDurationEncoder,
		},
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}
	if v.GetBool("server.development") {
		// If in development mode, default to development logger
		// configuration.
		d = zap.NewDevelopmentConfig()
	}
	if v.ConfigFileUsed() == "" {
		return d, nil
	}

	// Parsing yaml directly.
	raw := &cfgWrapper{}
	raw.Server.Log = d
	f, openErr := os.Open(v.ConfigFileUsed())
	if openErr != nil {
		return d, openErr
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.Println("failed to close config file:", closeErr)
		}
	}()
	buf, readErr := ioutil.ReadAll(f)
	if readErr != nil {
		return d, readErr
	}
	return raw.Server.Log, yaml.Unmarshal(buf, &raw)
}

func parseFilteringRules(v *viper.Viper, parentLogger *zap.Logger, key string) (*filter.List, error) {
	l := parentLogger.Named(key)
	type rawRuleItem struct {
		Net    string `mapstructure:"net"`
		Action string `mapstructure:"action"`
	}
	var rawRules []rawRuleItem
	if keyErr := v.UnmarshalKey("filter."+key+".rules", &rawRules); keyErr != nil {
		l.Error("failed to parse rules", zap.Error(keyErr))
		return nil, keyErr
	}
	var rules []filter.Rule
	for _, rawRule := range rawRules {
		var (
			action filter.Action
		)
		switch strings.ToLower(rawRule.Action) {
		case "allow":
			action = filter.Allow
		case "drop", "forbid", "deny", "block":
			action = filter.Deny
		case "pass", "none", "":
			action = filter.Pass
		default:
			l.Error("failed to parse action", zap.String("action", rawRule.Action))
			return nil, fmt.Errorf("unknown action %s", rawRule.Action)
		}
		rule, ruleErr := filter.StaticNetRule(action, rawRule.Net)
		if ruleErr != nil {
			l.Error("failed to parse subnet",
				zap.Error(ruleErr), zap.String("net", rawRule.Net),
			)
			return nil, ruleErr
		}
		l.Info("added rule",
			zap.Stringer("action", action),
			zap.String("net", rawRule.Net),
		)
		rules = append(rules, rule)
	}
	defaultAction := filter.Allow
	switch strings.ToLower(v.GetString("filter." + key + ".action")) {
	case "allow", "":
		// Same as default.
	case "drop", "forbid", "deny", "block":
		defaultAction = filter.Deny
	case "pass", "none":
		return nil, errors.New("default action cannot be pass")
	default:
		return nil, errors.New("unknown default action")
	}
	l.Info("default action set", zap.Stringer("action", defaultAction))
	f := filter.NewFilter(defaultAction, rules...)
	return f, nil
}

const keyPrometheusActive = "server.prometheus.active"

func parseOptions(v *viper.Viper, l *zap.Logger, o *server.Options) error {
	o.Realm = v.GetString("server.realm")
	o.Workers = v.GetInt("server.workers")
	o.AuthForSTUN = v.GetBool("auth.stun")
	o.Software = v.GetString("server.software")
	o.ReusePort = v.GetBool("server.reuseport")
	o.DebugCollect = v.GetBool("server.debug.collect")
	o.MetricsEnabled = v.GetBool(keyPrometheusActive)
	filterLog := l.Named("filter")
	var parseErr error
	if o.PeerRule, parseErr = parseFilteringRules(v, filterLog, "peer"); parseErr != nil {
		l.Error("failed to parse peer rules", zap.Error(parseErr))
		return parseErr
	}
	if o.ClientRule, parseErr = parseFilteringRules(v, filterLog, "client"); parseErr != nil {
		l.Error("failed to parse client rules", zap.Error(parseErr))
		return parseErr
	}
	if o.Software != "" {
		l.Info("will be sending SOFTWARE attribute", zap.String("software", o.Software))
	}
	return nil
}

func parseStaticCredentials(v *viper.Viper, l *zap.Logger, realm string) []auth.StaticCredential {
	// Parsing static credentials.
	var staticCredentials []auth.StaticCredential
	var rawCredentials []staticCredElem
	if keyErr := v.UnmarshalKey("auth.static", &rawCredentials); keyErr != nil {
		l.Fatal("failed to parse auth.static config", zap.Error(keyErr))
	}
	for _, cred := range rawCredentials {
		var a auth.StaticCredential
		if cred.Realm == "" {
			cred.Realm = realm
		}
		if strings.HasPrefix(cred.Key, "0x") {
			key, decodeErr := hex.DecodeString(cred.Key[2:])
			if decodeErr != nil {
				l.Error("failed to parse credential",
					zap.String("cred", fmt.Sprintf("%+v", cred)),
					zap.Error(decodeErr),
				)
			}
			a.Key = key
		}
		a.Username = cred.Username
		a.Password = cred.Password
		a.Realm = cred.Realm
		staticCredentials = append(staticCredentials, a)
	}
	return staticCredentials
}

func getListeners(v *viper.Viper, l *zap.Logger) []listener {
	if cfgPath := v.ConfigFileUsed(); len(cfgPath) > 0 {
		l.Info("config file used", zap.String("path", v.ConfigFileUsed()))
	} else {
		l.Info("default configuration used")
	}
	if strings.Split(v.GetString("version"), ".")[0] != "1" {
		l.Fatal("unsupported config file version", zap.String("v", v.GetString("version")))
	}
	reg := prometheus.NewPedanticRegistry()
	if prometheusAddr := v.GetString("server.prometheus.addr"); prometheusAddr != "" {
		l.Warn("running prometheus metrics", zap.String("addr", prometheusAddr))
		go func() {
			promHandler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{
				ErrorLog:      zap.NewStdLog(l),
				ErrorHandling: promhttp.HTTPErrorOnError,
			})
			if listenErr := http.ListenAndServe(prometheusAddr, promHandler); listenErr != nil {
				l.Error("prometheus failed to listen",
					zap.String("addr", prometheusAddr),
					zap.Error(listenErr),
				)
			}
		}()
	} else {
		v.SetDefault(keyPrometheusActive, false)
		if v.GetBool(keyPrometheusActive) {
			l.Warn("ignoring " + keyPrometheusActive + " because prometheus http endpoint is not configured")
		}
	}
	if pprofAddr := v.GetString("server.pprof"); pprofAddr != "" {
		l.Warn("running pprof", zap.String("addr", pprofAddr))
		go func() {
			pprofMux := http.NewServeMux()
			pprofMux.HandleFunc("/debug/pprof/", pprof.Index)
			pprofMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			pprofMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
			pprofMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			pprofMux.HandleFunc("/debug/pprof/trace", pprof.Trace)
			if listenErr := http.ListenAndServe(pprofAddr, pprofMux); listenErr != nil {
				l.Error("pprof failed to listen",
					zap.String("addr", pprofAddr),
					zap.Error(listenErr),
				)
			}
		}()
	}
	realm := v.GetString("server.realm") // default realm
	staticCredentials := parseStaticCredentials(v, l, realm)
	l.Info("parsed credentials", zap.Int("n", len(staticCredentials)))
	l.Info("realm", zap.String("k", realm))
	o := server.Options{
		Log:      l,
		Registry: reg,
	}
	if v.GetBool("auth.public") {
		l.Warn("auth is public")
	} else {
		o.Auth = auth.NewStatic(staticCredentials)
	}
	if parseErr := parseOptions(v, l, &o); parseErr != nil {
		l.Fatal("failed to parse", zap.Error(parseErr))
	}
	u := server.NewUpdater(o)
	n := reload.NewNotifier(l.Named("reload"))
	go func() {
		for range n.C {
			l.Info("trying to update config")
			if readErr := v.ReadInConfig(); readErr != nil {
				l.Error("failed to read config", zap.Error(readErr))
				continue
			}
			l.Info("config read", zap.String("path", v.ConfigFileUsed()))
			newOptions := server.Options{
				Log:      l,
				Registry: reg,
			}
			if parseErr := parseOptions(v, l, &newOptions); parseErr != nil {
				l.Error("failed to parse config", zap.Error(parseErr))
				continue
			}
			u.Set(newOptions)
			l.Info("config updated")
		}
	}()
	if apiAddr := v.GetString("api.addr"); apiAddr != "" {
		m := manage.NewManager(l.Named("api"), n)
		l.Info("api listening", zap.String("addr", apiAddr))
		go func() {
			if listenErr := http.ListenAndServe(apiAddr, m); listenErr != nil {
				l.Error("failed to listen on management API addr",
					zap.String("addr", apiAddr),
					zap.Error(listenErr),
				)
			}
		}()
	}

	var toListen []listener
	for _, addr := range v.GetStringSlice("server.listen") {
		l.Info("got addr", zap.String("addr", addr))
		normalized := normalize(addr)
		if strings.HasPrefix(normalized, "0.0.0.0") {
			l.Warn("running on all interfaces")
			l.Warn("picking addr from ICE")
			addrs, iceErr := ice.Gather()
			if iceErr != nil {
				log.Fatal(iceErr)
			}
			for _, a := range addrs {
				l.Warn("got", zap.Stringer("a", a))
				if a.IP.IsLoopback() {
					continue
				}
				if a.IP.IsLinkLocalMulticast() || a.IP.IsLinkLocalUnicast() {
					continue
				}
				if a.IP.To4() == nil {
					continue
				}
				l.Warn("using", zap.Stringer("a", a))
				toListen = append(toListen, listener{
					adrr: strings.Replace(normalized, "0.0.0.0", a.IP.String(), -1),
					net:  "udp",
					u:    u,
				})
			}
		} else {
			toListen = append(toListen, listener{
				net:  "udp",
				adrr: normalized,
				u:    u,
			})
		}
	}

	return toListen
}

func getLogger(v *viper.Viper) *zap.Logger {
	logCfg, logErr := getZapConfig(v)
	if logErr != nil {
		panic(logErr)
	}
	l, buildErr := logCfg.Build()
	if buildErr != nil {
		panic(buildErr)
	}
	return l
}

var rootCmd = &cobra.Command{
	Use:   "gortcd",
	Short: "gortcd is STUN and TURN server",
	Run: func(cmd *cobra.Command, args []string) {
		v := viper.GetViper()
		l := getLogger(v)
		wg := new(sync.WaitGroup)
		listeners := getListeners(v, l)
		wg.Add(len(listeners))
		for _, ln := range listeners {
			go func() {
				defer wg.Done()
				l.Info("gortc/gortcd listening", zap.String("addr", ln.adrr),
					zap.String("network", "udp"))
				if err := ListenUDPAndServe(ln.net, ln.adrr, ln.u); err != nil {
					l.Fatal("failed to listen", zap.Error(err))
				}
			}()
		}
		wg.Wait()
	},
}

type listener struct {
	net  string
	adrr string
	u    *server.Updater
}

var cfgFile string

func initConfigSnap(v *viper.Viper) {
	var (
		cfgRoot = os.Getenv("SNAP_USER_DATA")
	)
	cfgDir, err := os.Open(cfgRoot) // #nosec
	if err != nil {
		log.Fatalln("failed to open config directory:", err)
	}
	stat, statErr := cfgDir.Stat()
	if statErr != nil {
		log.Fatalln("failed to stat config directory:", statErr)
	}
	if !stat.IsDir() {
		log.Fatalln("the", cfgDir, "is not directory")
	}
	_, statErr = os.Stat(filepath.Join(cfgRoot, "gortcd.yml"))
	if statErr != nil {
		if !os.IsNotExist(statErr) {
			log.Fatalln("failed to stat config file:", statErr)
		}
		f, createErr := os.Create(filepath.Join(cfgRoot, "gortcd.yml"))
		if createErr != nil {
			log.Fatalln("failed to create initial config file:", createErr)
		}
		defer func() {
			if closeErr := f.Close(); closeErr != nil {
				log.Fatalln("failed to close config file:", closeErr)
			}
		}()
		if _, writeErr := fmt.Fprint(f, defaultConfigFileContent); writeErr != nil {
			log.Fatalln("failed to write default config file:", writeErr)
		}
	}
	v.AddConfigPath(cfgRoot)
}

func initConfigCommon(v *viper.Viper) {
	home, err := homedir.Dir()
	if err != nil {
		log.Fatalln("failed to find home directory:", err)
	}
	v.AddConfigPath(".")
	v.AddConfigPath("/etc/gortcd/")
	v.AddConfigPath(home)
}

func initConfig(v *viper.Viper) {
	// Don't forget to read config either from cfgFile or from home directory!
	if cfgFile != "" {
		// Use config file from the flag.
		v.SetConfigFile(cfgFile)
	} else {
		if os.Getenv("SNAP_NAME") != "" {
			initConfigSnap(v)
		} else {
			initConfigCommon(v)
		}
		v.SetConfigName("gortcd")
		v.SetConfigType("yaml")
	}
	cfgErr := v.ReadInConfig()
	if _, ok := cfgErr.(viper.ConfigFileNotFoundError); ok {
		cfgErr = v.ReadConfig(strings.NewReader(defaultConfigFileContent))
	}
	if cfgErr != nil {
		log.Fatalln("failed to read config:", cfgErr)
	}
}

func mustBind(err error) {
	if err != nil {
		log.Fatalln("failed to bind:", err)
	}
}

func initViper(v *viper.Viper) {
	v.SetDefault("server.workers", 100)
	v.SetDefault("auth.stun", false)
	v.SetDefault("version", "1")
	v.SetDefault("server.reuseport", true)
	v.SetDefault(keyPrometheusActive, true)
}

func init() {
	v := viper.GetViper()
	cobra.OnInitialize(func() {
		initConfig(v)
	})
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/gortcd.yml)")
	rootCmd.Flags().StringArrayP("listen", "l", []string{"0.0.0.0:3478"}, "listen address")
	rootCmd.Flags().String("pprof", "", "pprof address if specified")
	rootCmd.Flags().String("cpuprofile", "", "write cpu profile")
	mustBind(viper.BindPFlag("server.listen", rootCmd.Flags().Lookup("listen")))
	mustBind(viper.BindPFlag("server.pprof", rootCmd.Flags().Lookup("pprof")))
	mustBind(viper.BindPFlag("server.cpuprofile", rootCmd.Flags().Lookup("cpuprofile")))
	initViper(v)
}

// Execute starts root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
