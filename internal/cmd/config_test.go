package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestApplyCommandDefaultsHonorsExplicitFlags(t *testing.T) {
	command := &cobra.Command{Use: "build"}
	command.Flags().String("compress", "bzip2", "")
	command.Flags().Bool("strip", false, "")
	if err := command.Flags().Set("strip", "false"); err != nil {
		t.Fatal(err)
	}
	configuration := map[string]interface{}{
		"defaults": map[string]interface{}{
			"build": map[string]interface{}{"compress": "zstd", "strip": true},
		},
	}
	if err := applyCommandDefaults(command, configuration); err != nil {
		t.Fatal(err)
	}
	compress, _ := command.Flags().GetString("compress")
	strip, _ := command.Flags().GetBool("strip")
	if compress != "zstd" {
		t.Fatalf("compress = %q", compress)
	}
	if strip {
		t.Fatal("an explicitly supplied flag was overwritten")
	}
}

func TestExpandConfiguredPath(t *testing.T) {
	if got := expandConfiguredPath("~/.local/share/adpm", "/users/test"); got != "/users/test/.local/share/adpm" {
		t.Fatalf("expanded path = %q", got)
	}
}
