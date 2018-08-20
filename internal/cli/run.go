// Package cli implements command line interface for gortcd.
package cli

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/mitchellh/go-homedir"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"

	"github.com/gortc/gortcd/internal/auth"
	"github.com/gortc/gortcd/internal/server"
	"github.com/gortc/ice"
	"github.com/gortc/stun"
)

// ListenUDPAndServe listens on laddr and process incoming packets.
func ListenUDPAndServe(serverNet, laddr string, opt server.Options) error {
	c, err := net.ListenPacket(serverNet, laddr)
	if err != nil {
		return err
	}
	opt.Conn = c
	s, err := server.New(opt)
	if err != nil {
		return err
	}
	return s.Serve()
}

func normalize(address string) string {
	if len(address) == 0 {
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
func getZapConfig() (zap.Config, error) {
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
	if viper.GetBool("server.development") {
		// If in development mode, default to development logger
		// configuration.
		d = zap.NewDevelopmentConfig()
	}
	if len(viper.ConfigFileUsed()) == 0 {
		return d, nil
	}

	// Parsing yaml directly.
	raw := &cfgWrapper{}
	raw.Server.Log = d
	f, openErr := os.Open(viper.ConfigFileUsed())
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

var rootCmd = &cobra.Command{
	Use:   "gortcd",
	Short: "gortcd is STUN and TURN server",
	Run: func(cmd *cobra.Command, args []string) {
		logCfg, logErr := getZapConfig()
		if logErr != nil {
			panic(logErr)
		}
		l, buildErr := logCfg.Build()
		if buildErr != nil {
			panic(buildErr)
		}
		l.Info("config file used", zap.String("path", viper.ConfigFileUsed()))
		if strings.Split(viper.GetString("version"), ".")[0] != "1" {
			l.Fatal("unsupported config file version", zap.String("v", viper.GetString("version")))
		}
		reg := prometheus.NewPedanticRegistry()
		if prometheusAddr := viper.GetString("server.prometheus.addr"); prometheusAddr != "" {
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
		}
		if pprofAddr := viper.GetString("server.pprof"); pprofAddr != "" {
			l.Warn("running pprof", zap.String("addr", pprofAddr))
			go func() {
				if listenErr := http.ListenAndServe(pprofAddr, nil); listenErr != nil {
					l.Error("pprof failed to listen",
						zap.String("addr", pprofAddr),
						zap.Error(listenErr),
					)
				}
			}()
		}
		realm := viper.GetString("server.realm") // default realm
		// Parsing static credentials.
		var staticCredentials []auth.StaticCredential
		var rawCredentials []staticCredElem
		if keyErr := viper.UnmarshalKey("auth.static", &rawCredentials); keyErr != nil {
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
		l.Info("parsed credentials", zap.Int("n", len(staticCredentials)))
		l.Info("realm", zap.String("k", realm))
		o := server.Options{
			Realm:       realm,
			Log:         l,
			Workers:     viper.GetInt("server.workers"),
			Registry:    reg,
			AuthForSTUN: viper.GetBool("auth.stun"),
			Software:    viper.GetString("server.software"),
		}
		if o.Software != "" {
			l.Info("will be sending SOFTWARE attribute", zap.String("software", o.Software))
		}
		if viper.GetBool("auth.public") {
			l.Warn("auth is public")
		} else {
			o.Auth = auth.NewStatic(staticCredentials)
		}
		wg := new(sync.WaitGroup)
		for _, addr := range viper.GetStringSlice("server.listen") {
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
					wg.Add(1)
					go func(addr string) {
						defer wg.Done()
						l.Info("gortc/gortcd listening",
							zap.String("addr", addr),
							zap.String("network", "udp"),
						)
						if lErr := ListenUDPAndServe("udp", addr, o); lErr != nil {
							l.Fatal("failed to listen", zap.Error(lErr))
						}
					}(strings.Replace(normalized, "0.0.0.0", a.IP.String(), -1))
				}
			} else {
				l.Info("gortc/gortcd listening",
					zap.String("addr", normalized),
					zap.String("network", "udp"),
				)
				wg.Add(1)
				go func() {
					defer wg.Done()
					if logErr = ListenUDPAndServe("udp", normalized, o); logErr != nil {
						l.Fatal("failed to listen", zap.Error(logErr))
					}
				}()
			}
		}
		wg.Wait()
	},
}

var cfgFile string

func initConfig() {
	// Don't forget to read config either from cfgFile or from home directory!
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		viper.AddConfigPath(".")
		viper.AddConfigPath("/etc/gortcd/")
		viper.AddConfigPath(home)
		viper.SetConfigName("gortcd")
		viper.SetConfigType("yaml")
	}

	if err := viper.ReadInConfig(); err != nil {
		fmt.Println("Can't read config:", err)
		os.Exit(1)
	}
}

func mustBind(err error) {
	if err != nil {
		log.Fatalln("failed to bind:", err)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/gortcd.yml)")
	rootCmd.Flags().StringArrayP("listen", "l", []string{"0.0.0.0:3478"}, "listen address")
	rootCmd.Flags().String("pprof", "", "pprof address if specified")
	mustBind(viper.BindPFlag("server.listen", rootCmd.Flags().Lookup("listen")))
	mustBind(viper.BindPFlag("server.pprof", rootCmd.Flags().Lookup("pprof")))
	viper.SetDefault("server.workers", runtime.GOMAXPROCS(0))
	viper.SetDefault("server.software", "gortc/gortcd")
	viper.SetDefault("auth.stun", false)
	viper.SetDefault("version", "1")
}

// Execute starts root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
