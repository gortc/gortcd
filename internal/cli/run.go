// Package cli implements command line interface for gortcd.
package cli

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"sync"
	"syscall"

	"github.com/libp2p/go-reuseport"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"gortc.io/stun"

	"gortc.io/gortcd/internal/auth"
	"gortc.io/gortcd/internal/filter"
	"gortc.io/gortcd/internal/manage"
	"gortc.io/gortcd/internal/reload"
	"gortc.io/gortcd/internal/server"
	"gortc.io/ice"
)

// ListenUDPAndServe listens on laddr and process incoming packets.
func ListenUDPAndServe(log *zap.Logger, serverNet, laddr string, u *server.Updater) error {
	var (
		c   net.PacketConn
		err error
	)
	opt := u.Get()
	if reuseport.Available() && opt.ReusePort {
		c, err = reuseport.ListenPacket(serverNet, laddr)
		if err != nil {
			// Trying to listen without reuseport.
			// Sometimes reuseport.Available() can be true, but for subset
			// of interfaces it is not available.
			reusePortErr := err
			c, err = net.ListenPacket(serverNet, laddr)
			if err == nil {
				opt.ReusePort = false
				log.Warn("failed to use REUSEPORT, falling back to non-reuseport", zap.Error(reusePortErr))
			}
		}
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
					fromAny: true,
					adrr:    strings.Replace(normalized, "0.0.0.0", a.IP.String(), -1),
					net:     "udp",
					u:       u,
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

func protocolNotSupported(err error) bool {
	switch err := err.(type) {
	case syscall.Errno:
		switch err {
		case syscall.EPROTONOSUPPORT, syscall.ENOPROTOOPT:
			return true
		}
	case *os.SyscallError:
		return protocolNotSupported(err.Err)
	case *net.OpError:
		return protocolNotSupported(err.Err)
	}
	return false
}

func runRoot(v *viper.Viper, listenFunc func(log *zap.Logger, serverNet, laddr string, u *server.Updater) error) {
	l := getLogger(v)
	wg := new(sync.WaitGroup)
	listeners := getListeners(v, l)
	wg.Add(len(listeners))
	for _, lr := range listeners {
		go func(ln listener) {
			defer wg.Done()
			lg := l.With(zap.String("addr", ln.adrr), zap.String("network", "udp"))
			lg.Info("gortc/gortcd listening")
			if err := listenFunc(lg, ln.net, ln.adrr, ln.u); err != nil {
				if ln.fromAny && protocolNotSupported(err) {
					// See https://gortc.io/gortcd/issues/32
					// Should be ok to make it non configurable.
					lg.Warn("failed to listen", zap.Error(err))
				} else {
					lg.Fatal("failed to listen", zap.Error(err))
				}
			}
		}(lr)
	}
	wg.Wait()
}

func getRoot(v *viper.Viper, listenFunc func(log *zap.Logger, serverNet, laddr string, u *server.Updater) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:              "gortcd",
		Short:            "gortcd is STUN and TURN server",
		PersistentPreRun: func(cmd *cobra.Command, args []string) { initConfig(v) },
		Run:              func(cmd *cobra.Command, args []string) { runRoot(v, listenFunc) },
	}

	cmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/gortcd.yml)")
	cmd.Flags().StringSliceP("listen", "l", []string{"0.0.0.0:3478"}, "listen address")
	cmd.Flags().String("pprof", "", "pprof address if specified")
	cmd.Flags().String("cpuprofile", "", "write cpu profile")

	mustBind(v.BindPFlag("server.listen", cmd.Flags().Lookup("listen")))
	mustBind(v.BindPFlag("server.pprof", cmd.Flags().Lookup("pprof")))
	mustBind(v.BindPFlag("server.cpuprofile", cmd.Flags().Lookup("cpuprofile")))

	cmd.AddCommand(getReloadCmd(v))
	cmd.AddCommand(getKeyCmd())

	return cmd
}

type listener struct {
	net     string
	adrr    string
	u       *server.Updater
	fromAny bool // as part of 0.0.0.0
}
