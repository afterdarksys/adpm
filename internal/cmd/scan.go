package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/afterdarksys/adpm/internal/pkgarchive"
	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan [archive.adpm]",
	Short: "Scan an ADPM package for vulnerabilities using its SBOM",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		archivePath, _ := filepath.Abs(args[0])

		if _, err := os.Stat(archivePath); err != nil {
			fmt.Printf("Error: Cannot find archive %s\n", archivePath)
			os.Exit(1)
		}

		fmt.Printf("Scanning package: %s\n", archivePath)

		// 1. Extract the package into a temp directory to get META.json
		tempDir, err := os.MkdirTemp("", "adpm_scan_")
		if err != nil {
			fmt.Printf("Error creating temp dir: %v\n", err)
			os.Exit(1)
		}
		defer os.RemoveAll(tempDir)

		if err := pkgarchive.ExtractAuto(archivePath, tempDir); err != nil {
			fmt.Printf("Error: Cannot safely extract package: %v\n", err)
			os.Exit(1)
		}

		metaPath := filepath.Join(tempDir, "META.json")
		if _, err := os.Stat(metaPath); err != nil {
			fmt.Println("Error: Invalid ADPM package. META.json not found.")
			os.Exit(1)
		}

		metaBytes, err := os.ReadFile(metaPath)
		if err != nil {
			fmt.Printf("Error reading metadata: %v\n", err)
			os.Exit(1)
		}

		var meta struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			SBOM    struct {
				Components []struct {
					Name string `json:"name"`
					Type string `json:"type"`
					Purl string `json:"purl"`
				} `json:"components"`
			} `json:"sbom"`
		}

		if err := json.Unmarshal(metaBytes, &meta); err != nil {
			fmt.Printf("Error parsing metadata: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("============================================")
		fmt.Printf("  ADPM Scan Results: %s v%s\n", meta.Name, meta.Version)
		fmt.Println("============================================")

		if len(meta.SBOM.Components) == 0 {
			fmt.Println("\nNotice: No SBOM found or no components listed in package.")
			fmt.Println("        Did you build this package with --generate-sbom?")
		} else {
			fmt.Println("\nEmbedded Bill of Materials (SBOM):")
			for _, comp := range meta.SBOM.Components {
				fmt.Printf("  - %s: %s (PURL: %s)\n", comp.Type, comp.Name, comp.Purl)
			}
		}

		// Check if trivy is installed
		if _, err := exec.LookPath("trivy"); err == nil {
			fmt.Println("\n[INFO] Trivy scanner detected. Launching deep filesystem scan on payload...")

			trivyCmd := exec.Command("trivy", "fs", tempDir)
			trivyCmd.Stdout = os.Stdout
			trivyCmd.Stderr = os.Stderr

			if err := trivyCmd.Run(); err != nil {
				fmt.Println("\n[WARN] Trivy scan found issues or returned an error code.")
				if failOnVuln, _ := cmd.Flags().GetBool("fail-on-violation"); failOnVuln {
					os.Exit(1)
				}
			}
		} else {
			fmt.Println("\n[INFO] 'trivy' scanner not found on system. Dependency audit complete.")
			fmt.Println("       Install trivy (https://aquasecurity.github.io/trivy) for full CVE scanning.")
		}
	},
}

func init() {
	rootCmd.AddCommand(scanCmd)
	scanCmd.Flags().Bool("fail-on-violation", false, "Exit with non-zero code if vulnerabilities are found")
}
