package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "adpm",
	Short: "AfterDark Package Manager (ADPM) CLI",
	Long: `ADPM - AfterDark Package Manager
Homage to Todd Bennett III, unixeng.

A lightweight package manager for bundling complex dependencies 
(especially C libraries) with Python and Go projects as cross-platform closures.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
