package cli

import (
	"testing"
)

func TestGetKey(t *testing.T) {
	flags := getKeyCmd().Flags()
	_ = flags.Set("user", "user")
	_ = flags.Set("password", "secret")
	_ = flags.Set("realm", "realm")
	if h := getIntegrityHexFromFlags(flags); h != "fb6cb9e166c6c764ff2bdea12175a8aa" {
		t.Errorf("bad integrity %s", h)
	}
}
