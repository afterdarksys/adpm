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
	if opts.OutPkg == "adpm" {
		switch opts.InPkg {
		case "rpm":
			return convertRpmToAdpm(opts)
		case "deb":
			return convertDebToAdpm(opts)
		case "apk":
			return convertApkToAdpm(opts)
		case "tar.gz", "tgz":
			return convertTarGzToAdpm(opts)
		}
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

func convertDebToAdpm(opts ConversionOptions) error {
	fmt.Printf("Starting conversion: %s (DEB) -> ADPM\n", opts.Input)

	// Create temp staging area
	tempDir, err := os.MkdirTemp("", "adpm_convert_deb_")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	inputPath, err := filepath.Abs(opts.Input)
	if err != nil {
		return err
	}

	// Extract DEB using dpkg-deb or ar+tar
	fmt.Println("Extracting DEB archive...")

	// Try dpkg-deb first (cleaner)
	if _, err := exec.LookPath("dpkg-deb"); err == nil {
		cmd := exec.Command("dpkg-deb", "-x", inputPath, tempDir)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("dpkg-deb extraction failed: %w", err)
		}
	} else {
		// Fallback to ar + tar
		arCmd := exec.Command("ar", "x", inputPath)
		arCmd.Dir = tempDir
		if err := arCmd.Run(); err != nil {
			return fmt.Errorf("ar extraction failed: %w", err)
		}

		// Extract data.tar.*
		dataFiles, _ := filepath.Glob(filepath.Join(tempDir, "data.tar.*"))
		if len(dataFiles) == 0 {
			return fmt.Errorf("no data.tar.* found in DEB")
		}

		tarCmd := exec.Command("tar", "-xf", filepath.Base(dataFiles[0]))
		tarCmd.Dir = tempDir
		if err := tarCmd.Run(); err != nil {
			return fmt.Errorf("tar extraction failed: %w", err)
		}
	}

	// Extract metadata
	fmt.Println("Extracting DEB metadata...")
	name, version, err := extractDebMetadata(inputPath)
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

	// Find binaries and libraries
	var binaries []string
	var libraries []string

	for _, binDir := range []string{"usr/bin", "bin", "usr/sbin", "sbin"} {
		dirPath := filepath.Join(tempDir, binDir)
		if info, err := os.Stat(dirPath); err == nil && info.IsDir() {
			binaries = append(binaries, dirPath)
		}
	}

	for _, libDir := range []string{"usr/lib", "usr/lib64", "usr/lib/x86_64-linux-gnu", "usr/lib/aarch64-linux-gnu", "lib", "lib64"} {
		dirPath := filepath.Join(tempDir, libDir)
		if info, err := os.Stat(dirPath); err == nil && info.IsDir() {
			libraries = append(libraries, dirPath)
		}
	}

	if len(binaries) == 0 && len(libraries) == 0 {
		fmt.Println("Warning: No standard bin/ or lib/ directories found in DEB payload.")
	}

	// Build ADPM package
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

func extractDebMetadata(debPath string) (name string, version string, err error) {
	// Try dpkg-deb first
	if _, err := exec.LookPath("dpkg-deb"); err == nil {
		cmd := exec.Command("dpkg-deb", "-f", debPath, "Package", "Version")
		out, err := cmd.Output()
		if err != nil {
			return "", "", err
		}

		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "Package:") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "Package:"))
			} else if strings.HasPrefix(line, "Version:") {
				version = strings.TrimSpace(strings.TrimPrefix(line, "Version:"))
			}
		}

		if name != "" && version != "" {
			return name, version, nil
		}
	}

	return "", "", fmt.Errorf("could not extract DEB metadata")
}

func convertApkToAdpm(opts ConversionOptions) error {
	fmt.Printf("Starting conversion: %s (APK) -> ADPM\n", opts.Input)

	tempDir, err := os.MkdirTemp("", "adpm_convert_apk_")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	inputPath, err := filepath.Abs(opts.Input)
	if err != nil {
		return err
	}

	// APK is just a gzipped tar archive
	fmt.Println("Extracting APK archive...")
	tarCmd := exec.Command("tar", "-xzf", inputPath)
	tarCmd.Dir = tempDir
	if err := tarCmd.Run(); err != nil {
		return fmt.Errorf("apk extraction failed: %w", err)
	}

	// Extract metadata from .PKGINFO if it exists
	name := opts.Name
	version := opts.Version

	pkginfoPath := filepath.Join(tempDir, ".PKGINFO")
	if data, err := os.ReadFile(pkginfoPath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "pkgname = ") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "pkgname = "))
			} else if strings.HasPrefix(line, "pkgver = ") {
				version = strings.TrimSpace(strings.TrimPrefix(line, "pkgver = "))
			}
		}
	}

	if name == "" {
		name = "converted-package"
	}
	if version == "" {
		version = "1.0.0"
	}

	// Find binaries and libraries
	var binaries []string
	var libraries []string

	for _, binDir := range []string{"usr/bin", "bin", "usr/sbin", "sbin"} {
		dirPath := filepath.Join(tempDir, binDir)
		if info, err := os.Stat(dirPath); err == nil && info.IsDir() {
			binaries = append(binaries, dirPath)
		}
	}

	for _, libDir := range []string{"usr/lib", "lib"} {
		dirPath := filepath.Join(tempDir, libDir)
		if info, err := os.Stat(dirPath); err == nil && info.IsDir() {
			libraries = append(libraries, dirPath)
		}
	}

	// Build ADPM package
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

func convertTarGzToAdpm(opts ConversionOptions) error {
	fmt.Printf("Starting conversion: %s (tar.gz) -> ADPM\n", opts.Input)

	tempDir, err := os.MkdirTemp("", "adpm_convert_tgz_")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	inputPath, err := filepath.Abs(opts.Input)
	if err != nil {
		return err
	}

	// Extract tar.gz
	fmt.Println("Extracting tar.gz archive...")
	tarCmd := exec.Command("tar", "-xzf", inputPath)
	tarCmd.Dir = tempDir
	if err := tarCmd.Run(); err != nil {
		return fmt.Errorf("tar extraction failed: %w", err)
	}

	name := opts.Name
	version := opts.Version

	if name == "" {
		// Derive name from filename
		base := filepath.Base(opts.Input)
		name = strings.TrimSuffix(base, filepath.Ext(base))
		name = strings.TrimSuffix(name, ".tar")
	}
	if version == "" {
		version = "1.0.0"
	}

	// Find binaries and libraries
	var binaries []string
	var libraries []string

	for _, binDir := range []string{"usr/bin", "bin", "usr/sbin", "sbin"} {
		dirPath := filepath.Join(tempDir, binDir)
		if info, err := os.Stat(dirPath); err == nil && info.IsDir() {
			binaries = append(binaries, dirPath)
		}
	}

	for _, libDir := range []string{"usr/lib", "usr/lib64", "lib", "lib64"} {
		dirPath := filepath.Join(tempDir, libDir)
		if info, err := os.Stat(dirPath); err == nil && info.IsDir() {
			libraries = append(libraries, dirPath)
		}
	}

	// Build ADPM package
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
