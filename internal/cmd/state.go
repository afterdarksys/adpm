package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

func stateDB(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if value := os.Getenv("ADPM_DB"); value != "" {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve ADPM database: %w", err)
	}
	return filepath.Join(home, ".local", "share", "adpm"), nil
}

func runStateQuery(command, db string, args []string) error {
	tool, err := supportPath("installer/adpm_state.py")
	if err != nil {
		return err
	}
	stateArgs := []string{tool, command, "--db", db}
	if len(args) == 1 {
		stateArgs = append(stateArgs, "--package", args[0])
	}
	process := exec.Command("python3", stateArgs...)
	process.Stdout = os.Stdout
	process.Stderr = os.Stderr
	if err := process.Run(); err != nil {
		return fmt.Errorf("%s query failed: %w", command, err)
	}
	return nil
}

var historyCmd = &cobra.Command{
	Use:   "history [package]",
	Short: "Show the durable package and build event history",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbFlag, _ := cmd.Flags().GetString("db")
		db, err := stateDB(dbFlag)
		if err != nil {
			return err
		}
		return runStateQuery("history", db, args)
	},
}

var statusCmd = &cobra.Command{
	Use:   "status [package]",
	Short: "Show current ADPM-installed package state",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbFlag, _ := cmd.Flags().GetString("db")
		db, err := stateDB(dbFlag)
		if err != nil {
			return err
		}
		return runStateQuery("status", db, args)
	},
}

func init() {
	historyCmd.Flags().String("db", "", "Package database (default: ADPM_DB or ~/.local/share/adpm)")
	statusCmd.Flags().String("db", "", "Package database (default: ADPM_DB or ~/.local/share/adpm)")
	rootCmd.AddCommand(historyCmd, statusCmd)
}
