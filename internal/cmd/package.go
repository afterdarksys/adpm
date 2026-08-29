package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/afterdarksys/adpm/internal/pkgarchive"
	"github.com/spf13/cobra"
)

func loadPackage(path string) (string, map[string]interface{}, error) {
	tmp, err := os.MkdirTemp("", "adpm-package-")
	if err != nil {
		return "", nil, err
	}
	if err := pkgarchive.ExtractAuto(path, tmp); err != nil {
		os.RemoveAll(tmp)
		return "", nil, err
	}
	b, err := os.ReadFile(filepath.Join(tmp, "META.json"))
	if err != nil {
		os.RemoveAll(tmp)
		return "", nil, fmt.Errorf("META.json: %w", err)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(b, &meta); err != nil {
		os.RemoveAll(tmp)
		return "", nil, fmt.Errorf("META.json: %w", err)
	}
	return tmp, meta, nil
}

func validatePackage(path string) (map[string]interface{}, []string, error) {
	tmp, meta, err := loadPackage(path)
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(tmp)
	var warnings []string
	name, _ := meta["name"].(string)
	version, _ := meta["version"].(string)
	if strings.TrimSpace(name) == "" {
		return nil, nil, fmt.Errorf("metadata name is required")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+\-]*$`).MatchString(name) {
		return nil, nil, fmt.Errorf("unsafe package name %q", name)
	}
	if strings.TrimSpace(version) == "" {
		return nil, nil, fmt.Errorf("metadata version is required")
	}
	if native, ok := meta["native_dependencies"].(map[string]interface{}); ok {
		if values, ok := native["unresolved"].([]interface{}); ok && len(values) > 0 {
			missing := make([]string, 0, len(values))
			for _, value := range values {
				missing = append(missing, fmt.Sprint(value))
			}
			return nil, nil, fmt.Errorf("unresolved native dependencies: %s", strings.Join(missing, ", "))
		}
	}
	if _, err := os.Stat(filepath.Join(tmp, "INSTALL.sh")); err != nil {
		warnings = append(warnings, "INSTALL.sh is absent; the built-in installer will still install bin/lib payloads")
	}
	if _, bErr := os.Stat(filepath.Join(tmp, "bin")); bErr != nil {
		if _, lErr := os.Stat(filepath.Join(tmp, "lib")); lErr != nil {
			warnings = append(warnings, "package contains neither bin/ nor lib/")
		}
	}
	return meta, warnings, nil
}

var inspectCmd = &cobra.Command{
	Use: "inspect <package>", Short: "Show package metadata, contents summary, and SHA-256", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := filepath.Abs(args[0])
		if err != nil {
			return err
		}
		tmp, meta, err := loadPackage(path)
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		var files, bytes int64
		_ = filepath.Walk(tmp, func(_ string, info os.FileInfo, err error) error {
			if err == nil && info.Mode().IsRegular() {
				files++
				bytes += info.Size()
			}
			return nil
		})
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return err
		}
		result := map[string]interface{}{"path": path, "sha256": hex.EncodeToString(h.Sum(nil)), "files": files, "uncompressed_size": bytes, "metadata": meta}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			b, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(b))
			return nil
		}
		fmt.Printf("Package: %v v%v\nFiles: %d\nUncompressed size: %d bytes\nSHA-256: %s\n", meta["name"], meta["version"], files, bytes, result["sha256"])
		if p, ok := meta["platforms"]; ok {
			fmt.Printf("Platforms: %v\n", p)
		}
		return nil
	},
}

var validateCmd = &cobra.Command{
	Use: "validate <package>", Short: "Validate archive safety and ADPM metadata", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		meta, warnings, err := validatePackage(args[0])
		if err != nil {
			return err
		}
		for _, w := range warnings {
			fmt.Printf("warning: %s\n", w)
		}
		fmt.Printf("valid: %v v%v\n", meta["name"], meta["version"])
		return nil
	},
}

var mergeCmd = &cobra.Command{
	Use: "merge <package> <package>...", Short: "Merge platform-specific ADPM packages into one package", Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		stage, err := os.MkdirTemp("", "adpm-merge-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		var merged map[string]interface{}
		platformSet := map[string]bool{}
		versionOverride, _ := cmd.Flags().GetString("version")
		for _, arg := range args {
			tmp, meta, err := loadPackage(arg)
			if err != nil {
				return fmt.Errorf("%s: %w", arg, err)
			}
			if merged == nil {
				merged = meta
			} else if fmt.Sprint(merged["name"]) != fmt.Sprint(meta["name"]) {
				os.RemoveAll(tmp)
				return fmt.Errorf("package name mismatch: %v and %v", merged["name"], meta["name"])
			} else if versionOverride == "" && fmt.Sprint(merged["version"]) != fmt.Sprint(meta["version"]) {
				os.RemoveAll(tmp)
				return fmt.Errorf("version mismatch: %v and %v (use --version to override)", merged["version"], meta["version"])
			}
			if values, ok := meta["platforms"].([]interface{}); ok {
				for _, v := range values {
					platformSet[fmt.Sprint(v)] = true
				}
			}
			if err := mergeTree(tmp, stage); err != nil {
				os.RemoveAll(tmp)
				return fmt.Errorf("%s: %w", arg, err)
			}
			os.RemoveAll(tmp)
		}
		if versionOverride != "" {
			merged["version"] = versionOverride
		}
		for _, kind := range []string{"bin", "lib", "payload"} {
			entries, _ := os.ReadDir(filepath.Join(stage, kind))
			for _, e := range entries {
				if e.IsDir() {
					platformSet[e.Name()] = true
				}
			}
		}
		platforms := make([]string, 0, len(platformSet))
		for p := range platformSet {
			platforms = append(platforms, p)
		}
		sort.Strings(platforms)
		merged["platforms"] = platforms
		merged["packager"] = "After Dark Systems Package Manager"
		b, _ := json.MarshalIndent(merged, "", "  ")
		if err := os.WriteFile(filepath.Join(stage, "META.json"), b, 0644); err != nil {
			return err
		}
		output, _ := cmd.Flags().GetString("output")
		if output == "" {
			output = fmt.Sprintf("%v-%v.adpm", merged["name"], merged["version"])
		}
		output, err = filepath.Abs(output)
		if err != nil {
			return err
		}
		if err := pkgarchive.WriteCPIO(stage, output, "bzip2"); err != nil {
			return err
		}
		fmt.Printf("Merged %d packages (%s) -> %s\n", len(args), strings.Join(platforms, ", "), output)
		return nil
	},
}

func mergeTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." || rel == "META.json" {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if existing, err := os.ReadFile(target); err == nil {
			incoming, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if string(existing) != string(incoming) {
				return fmt.Errorf("conflicting file %s", rel)
			}
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		closeErr := out.Close()
		if err == nil {
			err = closeErr
		}
		return err
	})
}

func init() {
	rootCmd.AddCommand(inspectCmd, validateCmd, mergeCmd)
	inspectCmd.Flags().Bool("json", false, "Output machine-readable JSON")
	mergeCmd.Flags().StringP("output", "o", "", "Output .adpm file")
	mergeCmd.Flags().String("version", "", "Override the merged package version")
}
