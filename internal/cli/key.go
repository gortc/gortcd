package cli

import (
	"encoding/hex"
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"gortc.io/stun"
)

func getIntegrityHexFromFlags(f *pflag.FlagSet) string {
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
	return hex.EncodeToString(stun.NewLongTermIntegrity(u, r, p))
}

func getKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "generate long-term integrity key",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("0x%s\n", getIntegrityHexFromFlags(cmd.Flags()))
		},
	}
	cmd.Flags().StringP("user", "u", "", "username")
	cmd.Flags().StringP("password", "p", "", "password")
	cmd.Flags().StringP("realm", "r", "", "realm")

	return cmd
}
