package converter

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ConversionOptions struct {
	InPkg   string
	Input   string
	OutPkg  string
	Output  string
	Name    string
	Version string
}

func Convert(opts ConversionOptions) error {
	if opts.InPkg == "rpm" && opts.OutPkg == "adpm" {
		return convertRpmToAdpm(opts)
	}

	return fmt.Errorf("conversion from %s to %s is not currently supported", opts.InPkg, opts.OutPkg)
}

func convertRpmToAdpm(opts ConversionOptions) error {
	fmt.Printf("Starting conversion: %s (RPM) -> ADPM\n", opts.Input)

	// Create a temporary staging area for the RPM contents
	tempDir, err := os.MkdirTemp("", "adpm_convert_rpm_")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	inputPath, err := filepath.Abs(opts.Input)
	if err != nil {
		return err
	}

	// Extract RPM using rpm2cpio and cpio natively to avoid shell injection
	fmt.Println("Extracting RPM archive...")
	rpmCmd := exec.Command("rpm2cpio", inputPath)
	cpioCmd := exec.Command("cpio", "-idm", "--quiet")
	cpioCmd.Dir = tempDir

	pipe, err := rpmCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create pipe: %w", err)
	}
	cpioCmd.Stdin = pipe

	if err := rpmCmd.Start(); err != nil {
		return fmt.Errorf("rpm2cpio failed to start: %w", err)
	}
	if err := cpioCmd.Start(); err != nil {
		return fmt.Errorf("cpio failed to start: %w", err)
	}
	
	if err := rpmCmd.Wait(); err != nil {
		return fmt.Errorf("rpm2cpio failed: %w", err)
	}
	if err := cpioCmd.Wait(); err != nil {
		return fmt.Errorf("cpio failed: %w", err)
	}

	// Extract Metadata using rpm query
	fmt.Println("Extracting RPM metadata...")
	name, version, err := extractRpmMetadata(inputPath)
	if err != nil {
		fmt.Printf("Warning: Failed to extract metadata (%v). Using fallbacks.\n", err)
		name = opts.Name
		version = opts.Version
	}

	if name == "" {
		name = "converted-package"
	}
	if version == "" {
		version = "1.0.0"
	}

	// Find binaries and libraries to package
	var binaries []string
	var libraries []string

	// Check typical bin locations
	for _, binDir := range []string{"usr/bin", "bin", "usr/sbin", "sbin"} {
		dirPath := filepath.Join(tempDir, binDir)
		if info, err := os.Stat(dirPath); err == nil && info.IsDir() {
			binaries = append(binaries, dirPath)
		}
	}

	// Check typical lib locations
	for _, libDir := range []string{"usr/lib", "usr/lib64", "lib", "lib64"} {
		dirPath := filepath.Join(tempDir, libDir)
		if info, err := os.Stat(dirPath); err == nil && info.IsDir() {
			libraries = append(libraries, dirPath)
		}
	}

	if len(binaries) == 0 && len(libraries) == 0 {
		fmt.Println("Warning: No standard bin/ or lib/ directories found in RPM payload.")
	}

	// Build the ADPM package using adpm-build.py
	fmt.Printf("Building ADPM package %s v%s...\n", name, version)

	buildArgs := []string{"builder/adpm-build.py", "--name", name, "--version", version}
	if opts.Output != "" {
		buildArgs = append(buildArgs, "--output", opts.Output)
	}

	for _, b := range binaries {
		buildArgs = append(buildArgs, "--binaries", b+"/*")
	}
	for _, l := range libraries {
		buildArgs = append(buildArgs, "--libraries", l+"/*")
	}

	buildCmd := exec.Command("python3", buildArgs...)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr

	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("adpm build failed: %w", err)
	}

	fmt.Println("Conversion completed successfully!")
	return nil
}

// extractRpmMetadata attempts to use rpm to query package information
func extractRpmMetadata(rpmPath string) (name string, version string, err error) {
	// Query name and version
	cmd := exec.Command("rpm", "-qp", "--queryformat", "%{NAME}|%{VERSION}", rpmPath)
	out, err := cmd.Output()
	if err != nil {
		return "", "", err
	}

	parts := strings.Split(string(out), "|")
	if len(parts) >= 2 {
		return parts[0], parts[1], nil
	}
	return "", "", fmt.Errorf("unexpected rpm query output format")
}
