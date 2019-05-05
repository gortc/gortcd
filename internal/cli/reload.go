package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func execReload(v *viper.Viper, f *pflag.FlagSet, stdout io.Writer) {
	logCfg, logErr := getZapConfig(v)
	if logErr != nil {
		panic(logErr)
	}
	silent, err := f.GetBool("silent")
	if err != nil {
		panic(err)
	}
	if silent {
		// Override level to silent logs.
		logCfg.Level.SetLevel(zapcore.WarnLevel)
	}
	log, buildErr := logCfg.Build()
	if buildErr != nil {
		panic(buildErr)
	}
	l := log.Sugar()
	if cfgPath := v.ConfigFileUsed(); len(cfgPath) > 0 {
		l.Infow("config file used", "path", v.ConfigFileUsed())
	} else {
		l.Info("default configuration used")
	}
	if strings.Split(v.GetString("version"), ".")[0] != "1" {
		l.Fatalw("unsupported config file version", "v", v.GetString("version"))
	}
	apiAddr := v.GetString("api.addr")
	if apiAddr == "" {
		l.Fatal("no api.addr config set")
	}
	u := "http://" + apiAddr + "/reload"
	res, httpErr := http.Get(u) // #nosec
	if httpErr != nil {
		l.Fatalw("failed to perform http request", "err", httpErr)
	}
	if res.StatusCode != http.StatusOK {
		l.Fatalw("unexpected status code", "code", res.StatusCode, "status", res.Status)
	}
	body := new(bytes.Buffer)
	if _, err = io.Copy(body, res.Body); err != nil {
		l.Warn("failed to read body", "err", err)
	}
	if _, err = fmt.Fprintln(stdout, "OK", "-", strings.TrimSpace(body.String())); err != nil {
		l.Warn("write to stdout failed", zap.Error(err))
	}
}

func getReloadCmd(v *viper.Viper) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reload",
		Short: "notify server about config change via api",
		Run: func(cmd *cobra.Command, args []string) {
			execReload(v, cmd.Flags(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolP("silent", "s", true, "log only errors")
	return cmd
}
