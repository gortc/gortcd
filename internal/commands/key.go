package commands

import (
	"encoding/hex"
	"fmt"
	"log"

	"github.com/spf13/cobra"

	"github.com/gortc/stun"
)

var keyCmd = &cobra.Command{
	Use:   "key",
	Short: "generate long-term integrity key",
	Run: func(cmd *cobra.Command, args []string) {
		f := cmd.Flags()
		u, err := f.GetString("user")
		if err != nil {
			log.Fatal("failed to get user")
		}
		r, err := f.GetString("realm")
		if err != nil {
			log.Fatal("failed to get realm")
		}
		p, err := f.GetString("password")
		if err != nil {
			log.Fatal("failed to get password")
		}
		i := stun.NewLongTermIntegrity(u, r, p)
		fmt.Printf("0x%s\n", hex.EncodeToString(i))
	},
}

func init() {
	keyCmd.Flags().StringP("user", "u", "", "username")
	keyCmd.Flags().StringP("password", "p", "", "password")
	keyCmd.Flags().StringP("realm", "r", "", "realm")
	rootCmd.AddCommand(keyCmd)
}
