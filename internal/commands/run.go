// Package commands implements cli for gortcd.
package commands

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"encoding/hex"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"

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
	Password string `mapstructure:"username"`
	Key      string `mapstructure:"key"`
	Realm    string `mapstructure:"realm"`
}

var rootCmd = &cobra.Command{
	Use:   "gortcd",
	Short: "gortcd is STUN and TURN server",
	Run: func(cmd *cobra.Command, args []string) {
		logCfg := zap.NewDevelopmentConfig()
		logCfg.DisableCaller = true
		logCfg.DisableStacktrace = true
		l, err := logCfg.Build()
		if err != nil {
			panic(err)
		}
		if strings.Split(viper.GetString("version"), ".")[0] != "1" {
			l.Fatal("unsupported config file version", zap.String("v", viper.GetString("version")))
		}
		if pprofAddr := viper.GetString("server.pprof"); pprofAddr != "" {
			l.Warn("running pprof", zap.String("addr", pprofAddr))
			go func() {
				if err := http.ListenAndServe(pprofAddr, nil); err != nil {
					l.Error("pprof failed to listen",
						zap.String("addr", pprofAddr),
						zap.Error(err),
					)
				}
			}()
		}
		realm := viper.GetString("server.realm") // default realm
		// Parsing static credentials.
		var staticCredentials []auth.StaticCredential
		var rawCredentials []staticCredElem
		if err := viper.UnmarshalKey("auth.static", &rawCredentials); err != nil {
			l.Fatal("failed to parse auth.static config", zap.Error(err))
		}
		for _, cred := range rawCredentials {
			var a auth.StaticCredential
			if cred.Realm == "" {
				cred.Realm = realm
			}
			if strings.HasPrefix(cred.Key, "0x") {
				key, err := hex.DecodeString(cred.Key[2:])
				if err != nil {
					l.Error("failed to parse credential",
						zap.String("cred", fmt.Sprintf("%+v", cred)),
						zap.Error(err),
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
			Realm:   realm,
			Log:     l,
			Workers: viper.GetInt("server.workers"),
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
				addrs, err := ice.Gather()
				if err != nil {
					log.Fatal(err)
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
						if err = ListenUDPAndServe("udp", addr, o); err != nil {
							l.Fatal("failed to listen", zap.Error(err))
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
					if err = ListenUDPAndServe("udp", normalized, o); err != nil {
						l.Fatal("failed to listen", zap.Error(err))
					}
				}()
			}
		}
		wg.Wait()
		return
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

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/gortcd.yml)")
	rootCmd.Flags().StringArrayP("listen", "l", []string{"0.0.0.0:3478"}, "listen address")
	rootCmd.Flags().String("pprof", "", "pprof address if specified")
	viper.BindPFlag("server.listen", rootCmd.Flags().Lookup("listen"))
	viper.BindPFlag("server.pprof", rootCmd.Flags().Lookup("pprof"))
	viper.SetDefault("server.workers", 100)
	viper.SetDefault("version", "1")
}

// Execute starts root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
