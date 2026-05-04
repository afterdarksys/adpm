package cmd

import (
	"fmt"
	"os"

	"github.com/afterdarksys/adpm/internal/sysscript"
	"github.com/spf13/cobra"
)

var scriptCmd = &cobra.Command{
	Use:   "script <file.star>",
	Short: "Run a Starlark systems script with the sysscript engine",
	Long: `Run a .star file using the embedded sysscript engine.

The script receives a 'sys' global with the full systems API:

  sys.fs       — read, write, stat, glob, mkdir, rm, chmod
  sys.exec     — run(cmd, args...)
  sys.net      — http_get, dns_lookup, reverse_dns, port_check
  sys.proc     — list, get, find, kill
  sys.config   — write (atomic), template, validate, reload, backup, restore
  sys.yaml     — parse, encode
  sys.json     — parse, encode, encode_pretty
  sys.ini      — parse, encode, get
  sys.services — start, stop, restart, enable, disable, status
  sys.alerts   — push (stub)
  sys.security — yara_scan, scan_memory (stubs)
  sys.packages — install (stub)
  sys.containers — run (stub)

Entitlements are deny-by-default. Use flags to grant access:

  adpm script deploy.star \
    --allow-fs-read /etc \
    --allow-config-write /etc/postfix \
    --allow-service-reload postfix \
    --allow-net dnsscience.io`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		path := args[0]

		allowFSRead, _ := cmd.Flags().GetStringArray("allow-fs-read")
		allowFSWrite, _ := cmd.Flags().GetStringArray("allow-fs-write")
		allowNet, _ := cmd.Flags().GetStringArray("allow-net")
		allowConfigWrite, _ := cmd.Flags().GetStringArray("allow-config-write")
		allowServiceReload, _ := cmd.Flags().GetStringArray("allow-service-reload")
		allowExec, _ := cmd.Flags().GetBool("allow-exec")

		engine := sysscript.New(&sysscript.Entitlements{
			AllowFSRead:        allowFSRead,
			AllowFSWrite:       allowFSWrite,
			AllowNetOutbound:   allowNet,
			AllowConfigWrite:   allowConfigWrite,
			AllowServiceReload: allowServiceReload,
			AllowExec:          allowExec,
		})

		output, err := engine.ExecuteFile(path)
		if output != "" {
			fmt.Print(output)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "script error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(scriptCmd)

	scriptCmd.Flags().StringArray("allow-fs-read", []string{}, "Path prefixes allowed for fs.read/stat/glob (repeatable)")
	scriptCmd.Flags().StringArray("allow-fs-write", []string{}, "Path prefixes allowed for fs.write/mkdir/rm/chmod (repeatable)")
	scriptCmd.Flags().StringArray("allow-net", []string{}, "Hostnames/domains allowed for net.http_get (repeatable, * = all)")
	scriptCmd.Flags().StringArray("allow-config-write", []string{}, "Path prefixes allowed for atomic config.write (repeatable)")
	scriptCmd.Flags().StringArray("allow-service-reload", []string{}, "Service names allowed for services.* and config.reload (repeatable, * = all)")
	scriptCmd.Flags().Bool("allow-exec", false, "Allow sys.exec.run and sys.proc.kill")
}
