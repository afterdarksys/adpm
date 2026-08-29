package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

// supportPath locates a companion script without depending on the caller's cwd.
func supportPath(relative string) (string, error) {
	var roots []string
	if configured := os.Getenv("ADPM_HOME"); configured != "" {
		roots = append(roots, configured)
	}
	if executable, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(executable))
	}
	if cwd, err := os.Getwd(); err == nil {
		for current := cwd; ; current = filepath.Dir(current) {
			roots = append(roots, current)
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
		}
	}
	seen := map[string]bool{}
	for _, root := range roots {
		candidate := filepath.Join(root, filepath.FromSlash(relative))
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("ADPM support file %q not found; set ADPM_HOME to the installation directory", relative)
}
