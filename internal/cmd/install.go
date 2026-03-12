package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install [archive]",
	Short: "Install, uninstall, or list ADPM packages",
	Run: func(cmd *cobra.Command, args []string) {
		list, _ := cmd.Flags().GetBool("list")
		uninstall, _ := cmd.Flags().GetString("uninstall")
		upgrade, _ := cmd.Flags().GetString("upgrade")
		system, _ := cmd.Flags().GetBool("system")
		verify, _ := cmd.Flags().GetBool("verify")
		verifyReq, _ := cmd.Flags().GetBool("verify-required")

		execArgs := []string{"installer/adpm-install.sh"}

		if list {
			execArgs = append(execArgs, "--list")
		} else if uninstall != "" {
			execArgs = append(execArgs, "--uninstall", uninstall)
		} else if upgrade != "" {
			execArgs = append(execArgs, "--upgrade", upgrade)
		} else {
			if len(args) == 0 {
				fmt.Println("Error: must specify a package archive to install or an action flag.")
				cmd.Help()
				os.Exit(1)
			}
			execArgs = append(execArgs, args[0])
		}

		if system {
			execArgs = append(execArgs, "--system")
		}
		if verifyReq {
			execArgs = append(execArgs, "--verify-required")
		} else if verify {
			execArgs = append(execArgs, "--verify")
		}

		execCmd := exec.Command("bash", execArgs...)
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr

		if err := execCmd.Run(); err != nil {
			fmt.Printf("Install operation failed: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(installCmd)

	installCmd.Flags().Bool("list", false, "List installed packages")
	installCmd.Flags().String("uninstall", "", "Uninstall package by name")
	installCmd.Flags().String("upgrade", "", "Upgrade package from archive")
	installCmd.Flags().Bool("system", false, "Install system-wide (requires root)")
	installCmd.Flags().Bool("verify", false, "Verify package GPG signature before install")
	installCmd.Flags().Bool("verify-required", false, "Strictly verify package GPG signature (fails if missing)")
}
