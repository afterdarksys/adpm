package cmd

import (
	"github.com/afterdarksys/adpm/internal/converter"
	"github.com/spf13/cobra"
)

var convertCmd = &cobra.Command{
	Use:   "convert [input]",
	Short: "Convert between RPM, DEB, TGZ, BPM, ADPM, and CPIO formats",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		inPkg, _ := cmd.Flags().GetString("inpkg")
		input, _ := cmd.Flags().GetString("input")
		outPkg, _ := cmd.Flags().GetString("outpkg")
		output, _ := cmd.Flags().GetString("output")
		name, _ := cmd.Flags().GetString("name")
		version, _ := cmd.Flags().GetString("version")
		architecture, _ := cmd.Flags().GetString("arch")
		if input == "" && len(args) == 1 {
			input = args[0]
		}

		if inPkg == "" || input == "" || outPkg == "" {
			return cmd.Help()
		}

		opts := converter.ConversionOptions{
			InPkg:        inPkg,
			Input:        input,
			OutPkg:       outPkg,
			Output:       output,
			Name:         name,
			Version:      version,
			Architecture: architecture,
		}
		return converter.Convert(opts)
	},
}

func init() {
	rootCmd.AddCommand(convertCmd)

	convertCmd.Flags().String("inpkg", "", "Input format: rpm, deb, tgz, bpm, adpm, cpio[.gz|.bz2]")
	convertCmd.Flags().String("input", "", "Path to the input package file")
	convertCmd.Flags().String("outpkg", "", "Output format: rpm, deb, tgz, bpm, adpm, cpio[.gz|.bz2]")
	convertCmd.Flags().String("output", "dist", "Output directory or package filename")
	convertCmd.Flags().String("name", "", "Override package name (optional)")
	convertCmd.Flags().String("version", "", "Override package version (optional)")
	convertCmd.Flags().String("arch", "", "ADPM target platform override (for example linux-x86_64)")
}
