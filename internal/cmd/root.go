package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "adpm",
	Short: "After Dark Systems Package Manager",
	Long: `ADPM - After Dark Systems Package Manager
Homage to Todd Bennett III, unixeng.

A lightweight package manager for bundling complex dependencies 
(especially C libraries) with Python and Go projects as cross-platform closures.`,
	Version:       "0.3.0",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          func(cmd *cobra.Command, args []string) error { return cmd.Help() },
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
