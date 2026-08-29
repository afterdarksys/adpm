package cmd

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/afterdarksys/adpm/internal/pkgarchive"
	"github.com/spf13/cobra"
)

type PackageEntry struct {
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Architecture string                 `json:"architecture"`
	DistPath     string                 `json:"dist_path"`
	SHA256       string                 `json:"sha256"`
	BuiltAt      string                 `json:"built_at,omitempty"`
	ScannedAt    string                 `json:"scanned_at,omitempty"`
	CheckedAt    string                 `json:"checked_at"`
	Signature    string                 `json:"signature,omitempty"`
	Metadata     map[string]interface{} `json:"metadata"`
}

type CatalogIndex struct {
	Repository  string         `json:"repository"`
	GeneratedAt string         `json:"generated_at"`
	Packages    []PackageEntry `json:"packages"`
}

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage package repositories and catalogs",
}

var generateRepoCmd = &cobra.Command{
	Use:   "generate [directory]",
	Short: "Generate an index.json catalog for a directory of ADPM packages",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		targetDir, err := filepath.Abs(args[0])
		if err != nil {
			fmt.Printf("Error: Invalid directory path: %v\n", err)
			os.Exit(1)
		}

		if info, err := os.Stat(targetDir); err != nil || !info.IsDir() {
			fmt.Printf("Error: %s is not a valid directory\n", targetDir)
			os.Exit(1)
		}

		repoName, _ := cmd.Flags().GetString("name")
		sign, _ := cmd.Flags().GetBool("sign")
		key, _ := cmd.Flags().GetString("key")

		fmt.Printf("Scanning directory for ADPM packages: %s\n", targetDir)

		files, err := os.ReadDir(targetDir)
		if err != nil {
			fmt.Printf("Error reading directory: %v\n", err)
			os.Exit(1)
		}

		catalog := CatalogIndex{
			Repository:  repoName,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			Packages:    make([]PackageEntry, 0),
		}

		for _, file := range files {
			if file.IsDir() {
				continue
			}

			// Looking for valid ADPM archive extensions
			ext := filepath.Ext(file.Name())
			if ext != ".adpm" && ext != ".bz2" && ext != ".xz" && ext != ".gz" {
				continue
			}

			// We shouldn't process detached signatures directly
			if ext == ".asc" || ext == ".sha256" {
				continue
			}

			pkgPath := filepath.Join(targetDir, file.Name())
			fmt.Printf("  Processing %s...\n", file.Name())

			entry := processPackage(pkgPath)
			if entry != nil {
				catalog.Packages = append(catalog.Packages, *entry)
			}
		}

		fmt.Printf("Found %d valid packages. Writing index...\n", len(catalog.Packages))

		indexPath := filepath.Join(targetDir, "index.json")

		catalogBytes, err := json.MarshalIndent(catalog, "", "  ")
		if err != nil {
			fmt.Printf("Error generating JSON: %v\n", err)
			os.Exit(1)
		}

		if err := os.WriteFile(indexPath, catalogBytes, 0644); err != nil {
			fmt.Printf("Error writing index.json: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("✓ Created repository catalog: %s\n", indexPath)

		if sign {
			fmt.Println("Signing index.json with GPG...")

			// Remove old signature if it exists
			sigPath := indexPath + ".asc"
			os.Remove(sigPath)

			gpgArgs := []string{"gpg", "--detach-sign", "--armor"}
			if key != "" {
				gpgArgs = append(gpgArgs, "--default-key", key)
			}
			gpgArgs = append(gpgArgs, indexPath)

			gpgCmd := exec.Command(gpgArgs[0], gpgArgs[1:]...)
			gpgCmd.Stdout = os.Stdout
			gpgCmd.Stderr = os.Stderr

			if err := gpgCmd.Run(); err != nil {
				fmt.Printf("Warning: Failed to sign index.json. Is GPG configured?\n")
			} else {
				fmt.Printf("✓ Created cryptographic signature: %s\n", sigPath)
			}
		}

		fmt.Println("Repository generation complete!")
	},
}

func processPackage(archivePath string) *PackageEntry {
	// Calculate SHA256 of the archive
	file, err := os.Open(archivePath)
	if err != nil {
		fmt.Printf("    Warning: Could not open file for hashing: %v\n", err)
		return nil
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		fmt.Printf("    Warning: Failed to hash file: %v\n", err)
		return nil
	}
	sha256sum := fmt.Sprintf("%x", hash.Sum(nil))

	// Extract META.json
	tempDir, err := os.MkdirTemp("", "adpm_repo_ext_")
	if err != nil {
		fmt.Printf("    Warning: Temp dir failed: %v\n", err)
		return nil
	}
	defer os.RemoveAll(tempDir)

	if err := pkgarchive.ExtractAuto(archivePath, tempDir); err != nil {
		fmt.Printf("    Warning: Could not safely extract package: %v\n", err)
		return nil
	}

	metaPath := filepath.Join(tempDir, "META.json")
	if _, err := os.Stat(metaPath); err != nil {
		fmt.Printf("    Warning: Packages missing META.json. Skipping.\n")
		return nil
	}

	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return nil
	}

	var metaMap map[string]interface{}
	if err := json.Unmarshal(metaBytes, &metaMap); err != nil {
		fmt.Printf("    Warning: Malformed META.json. Skipping.\n")
		return nil
	}

	// Build the entry
	entry := &PackageEntry{
		DistPath:  filepath.Base(archivePath),
		SHA256:    sha256sum,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Metadata:  metaMap,
	}

	if name, ok := metaMap["name"].(string); ok {
		entry.Name = name
	}
	if version, ok := metaMap["version"].(string); ok {
		entry.Version = version
	}
	if arch, ok := metaMap["target_platform"].(string); ok {
		entry.Architecture = arch
	}

	// Check if there is a detached signature file alongside it
	sigPath := archivePath + ".asc"
	if _, err := os.Stat(sigPath); err == nil {
		entry.Signature = filepath.Base(sigPath)
	}

	// Example hook for extracting timestamps if they exist in metadata
	// Currently ADPM doesn't inject built_at natively, but we could shim it
	// For now we leave ScannedAt and BuiltAt empty unless injected elsewhere

	return entry
}

var searchRepoCmd = &cobra.Command{
	Use:   "search [directory-or-index.json]",
	Short: "Search a repository's index.json for packages",
	Long: `Search a repository catalog generated by 'adpm repo generate'.

Accepts either the repository directory (index.json is read from inside it)
or a direct path to an index.json file.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		target, err := filepath.Abs(args[0])
		if err != nil {
			fmt.Printf("Error: Invalid path: %v\n", err)
			os.Exit(1)
		}

		indexPath := target
		if info, err := os.Stat(target); err == nil && info.IsDir() {
			indexPath = filepath.Join(target, "index.json")
		}

		catalogBytes, err := os.ReadFile(indexPath)
		if err != nil {
			fmt.Printf("Error: Cannot read index (%s): %v\n", indexPath, err)
			fmt.Println("Hint: run 'adpm repo generate <directory>' first.")
			os.Exit(1)
		}

		var catalog CatalogIndex
		if err := json.Unmarshal(catalogBytes, &catalog); err != nil {
			fmt.Printf("Error: Malformed index.json: %v\n", err)
			os.Exit(1)
		}

		nameFilter, _ := cmd.Flags().GetString("name")
		versionFilter, _ := cmd.Flags().GetString("version")
		archFilter, _ := cmd.Flags().GetString("arch")
		asJSON, _ := cmd.Flags().GetBool("json")

		lowerName := strings.ToLower(nameFilter)
		lowerArch := strings.ToLower(archFilter)

		matches := make([]PackageEntry, 0)
		for _, pkg := range catalog.Packages {
			if lowerName != "" && !strings.Contains(strings.ToLower(pkg.Name), lowerName) {
				continue
			}
			if versionFilter != "" && pkg.Version != versionFilter {
				continue
			}
			if lowerArch != "" && strings.ToLower(pkg.Architecture) != lowerArch {
				continue
			}
			matches = append(matches, pkg)
		}

		if asJSON {
			out, err := json.MarshalIndent(matches, "", "  ")
			if err != nil {
				fmt.Printf("Error: Cannot encode results: %v\n", err)
				os.Exit(1)
			}
			fmt.Println(string(out))
			return
		}

		if len(matches) == 0 {
			fmt.Println("No packages matched.")
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tVERSION\tARCH\tSHA256\tSIGNED")
		for _, pkg := range matches {
			sha := pkg.SHA256
			if len(sha) > 12 {
				sha = sha[:12]
			}
			signed := "no"
			if pkg.Signature != "" {
				signed = "yes"
			}
			arch := pkg.Architecture
			if arch == "" {
				arch = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", pkg.Name, pkg.Version, arch, sha, signed)
		}
		w.Flush()
		fmt.Printf("\n%d package(s) matched (of %d in catalog).\n", len(matches), len(catalog.Packages))
	},
}

func init() {
	rootCmd.AddCommand(repoCmd)
	repoCmd.AddCommand(generateRepoCmd)
	repoCmd.AddCommand(searchRepoCmd)

	generateRepoCmd.Flags().String("name", "Local ADPM Repository", "Display name for the repository index")
	generateRepoCmd.Flags().Bool("sign", false, "Sign the resulting index.json with GPG")
	generateRepoCmd.Flags().String("key", "", "GPG Key ID to sign index with")

	searchRepoCmd.Flags().String("name", "", "Filter by package name (case-insensitive substring match)")
	searchRepoCmd.Flags().String("version", "", "Filter by exact version")
	searchRepoCmd.Flags().String("arch", "", "Filter by exact architecture")
	searchRepoCmd.Flags().Bool("json", false, "Output matches as JSON")
}
