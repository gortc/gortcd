package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap/zapcore"
)

var reloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "notify server about config change via api",
	Run: func(cmd *cobra.Command, args []string) {
		logCfg, logErr := getZapConfig()
		if logErr != nil {
			panic(logErr)
		}
		silent, err := cmd.Flags().GetBool("silent")
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
		if cfgPath := viper.ConfigFileUsed(); len(cfgPath) > 0 {
			l.Infow("config file used", "path", viper.ConfigFileUsed())
		} else {
			l.Info("default configuration used")
		}
		if strings.Split(viper.GetString("version"), ".")[0] != "1" {
			l.Fatalw("unsupported config file version", "v", viper.GetString("version"))
		}
		apiAddr := viper.GetString("api.addr")
		if apiAddr == "" {
			l.Fatal("no api.addr config set")
		}
		u := "http://" + apiAddr + "/reload"
		res, httpErr := http.Get(u) // #nosec
		if httpErr != nil {
			l.Fatalw("failed to perform http request", "err", httpErr)
		}
		if res.StatusCode != http.StatusOK {
			l.Fatalw("unexpected status code",
				"code", res.StatusCode, "status", res.Status,
			)
		}
		body := new(bytes.Buffer)
		if _, err = io.Copy(body, res.Body); err != nil {
			l.Warn("failed to read body", "err", err)
		}
		fmt.Println("OK", "-", strings.TrimSpace(body.String()))
	},
}

func init() {
	{
		f := reloadCmd.Flags()
		f.BoolP("silent", "s", true, "log only errors")
	}
	rootCmd.AddCommand(reloadCmd)
}
