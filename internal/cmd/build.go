package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build an ADPM package using adpm-build.py",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		version, _ := cmd.Flags().GetString("version")
		out, _ := cmd.Flags().GetString("output")
		platform, _ := cmd.Flags().GetString("platform")
		strip, _ := cmd.Flags().GetBool("strip")
		compress, _ := cmd.Flags().GetString("compress")
		sign, _ := cmd.Flags().GetBool("sign")
		key, _ := cmd.Flags().GetString("key")
		sbom, _ := cmd.Flags().GetBool("generate-sbom")
		detectDependencies, _ := cmd.Flags().GetBool("detect-dependencies")
		relocate, _ := cmd.Flags().GetBool("relocate")

		binaries, _ := cmd.Flags().GetStringSlice("binaries")
		libraries, _ := cmd.Flags().GetStringSlice("libraries")
		pythonPkgs, _ := cmd.Flags().GetStringSlice("python")
		dependencies, _ := cmd.Flags().GetStringArray("dependency")

		if name == "" || version == "" {
			return fmt.Errorf("--name and --version are required")
		}

		builderPath, err := supportPath("builder/adpm-build.py")
		if err != nil {
			return err
		}
		buildArgs := []string{builderPath, "--name", name, "--version", version}
		if out != "" {
			buildArgs = append(buildArgs, "--output", out)
		}
		if platform != "" {
			buildArgs = append(buildArgs, "--platform", platform)
		}
		if strip {
			buildArgs = append(buildArgs, "--strip")
		}
		if compress != "" {
			buildArgs = append(buildArgs, "--compress", compress)
		}
		if sign {
			buildArgs = append(buildArgs, "--sign")
			if key != "" {
				buildArgs = append(buildArgs, "--key", key)
			}
		}
		if sbom {
			buildArgs = append(buildArgs, "--generate-sbom")
		}
		if detectDependencies {
			buildArgs = append(buildArgs, "--detect-dependencies")
		}
		if relocate {
			buildArgs = append(buildArgs, "--relocate")
		}

		for _, b := range binaries {
			buildArgs = append(buildArgs, "--binaries", b)
		}
		for _, l := range libraries {
			buildArgs = append(buildArgs, "--libraries", l)
		}
		for _, p := range pythonPkgs {
			buildArgs = append(buildArgs, "--python", p)
		}
		for _, dependency := range dependencies {
			buildArgs = append(buildArgs, "--dependency", dependency)
		}

		execCmd := exec.Command("python3", buildArgs...)
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr

		if err := execCmd.Run(); err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(buildCmd)

	buildCmd.Flags().String("name", "", "Package name")
	buildCmd.Flags().String("version", "", "Package version")
	buildCmd.Flags().String("output", "dist", "Output directory")
	buildCmd.Flags().String("platform", "", "Target platform")
	buildCmd.Flags().Bool("strip", false, "Strip debug symbols")
	buildCmd.Flags().String("compress", "bzip2", "Compression algorithm (bzip2, gzip, xz, zstd)")
	buildCmd.Flags().Bool("sign", false, "GPG sign the resulting archive")
	buildCmd.Flags().String("key", "", "GPG key ID to use for signing")
	buildCmd.Flags().Bool("generate-sbom", false, "Generate and embed SBOM in package metadata")
	buildCmd.Flags().Bool("detect-dependencies", false, "Recursively detect and bundle non-system native libraries")
	buildCmd.Flags().Bool("relocate", false, "Rewrite native loader paths (implies --detect-dependencies)")

	buildCmd.Flags().StringSlice("binaries", []string{}, "Binaries to include")
	buildCmd.Flags().StringSlice("libraries", []string{}, "Libraries to include")
	buildCmd.Flags().StringSlice("python", []string{}, "Python packages to include")
	buildCmd.Flags().StringArray("dependency", []string{}, "ADPM dependency (NAME, NAME@CONSTRAINT, or NAME=VERSION; repeatable)")
}
