package cli

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"
)

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

func mustBind(err error) {
	if err != nil {
		log.Fatalln("failed to bind:", err)
	}
}

// TODO: Remove global state.
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

func initViper(v *viper.Viper) {
	v.SetDefault("server.workers", 100)
	v.SetDefault("auth.stun", false)
	v.SetDefault("version", "1")
	v.SetDefault("server.reuseport", true)
	v.SetDefault(keyPrometheusActive, true)
}

// Execute starts root command.
func Execute() {
	v := viper.GetViper()
	initViper(v)
	rootCmd := getRoot(v, ListenUDPAndServe)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
