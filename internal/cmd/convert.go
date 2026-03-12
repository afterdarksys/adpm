package cmd

import (
	"fmt"
	"os"

	"github.com/afterdarksys/adpm/internal/converter"
	"github.com/spf13/cobra"
)

var convertCmd = &cobra.Command{
	Use:   "convert",
	Short: "Convert between package formats (e.g. rpm -> adpm)",
	Run: func(cmd *cobra.Command, args []string) {
		inPkg, _ := cmd.Flags().GetString("inpkg")
		input, _ := cmd.Flags().GetString("input")
		outPkg, _ := cmd.Flags().GetString("outpkg")
		output, _ := cmd.Flags().GetString("output")
		name, _ := cmd.Flags().GetString("name")
		version, _ := cmd.Flags().GetString("version")

		if inPkg == "" || input == "" || outPkg == "" {
			fmt.Println("Error: --inpkg, --input, and --outpkg are required")
			cmd.Help()
			os.Exit(1)
		}

		opts := converter.ConversionOptions{
			InPkg:   inPkg,
			Input:   input,
			OutPkg:  outPkg,
			Output:  output,
			Name:    name,
			Version: version,
		}

		if err := converter.Convert(opts); err != nil {
			fmt.Printf("Conversion failed: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(convertCmd)

	convertCmd.Flags().String("inpkg", "", "Input package format (e.g. rpm)")
	convertCmd.Flags().String("input", "", "Path to the input package file")
	convertCmd.Flags().String("outpkg", "", "Output package format (e.g. adpm)")
	convertCmd.Flags().String("output", "adpm/packages", "Output directory")
	convertCmd.Flags().String("name", "", "Override package name (optional)")
	convertCmd.Flags().String("version", "", "Override package version (optional)")
}
