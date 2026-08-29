package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxConfigSize = 1024 * 1024

// CandidatePaths returns configuration locations from lowest to highest
// precedence. Empty overrides are omitted and duplicate paths are loaded once.
func CandidatePaths(name, home, environmentOverride, explicitOverride string) []string {
	candidates := []string{filepath.Join("/etc", "adpm", name)}
	if home != "" {
		candidates = append(candidates, filepath.Join(home, ".adpm", name))
	}
	candidates = append(candidates, environmentOverride, explicitOverride)
	seen := make(map[string]bool)
	paths := make([]string, 0, len(candidates))
	for _, path := range candidates {
		if path == "" {
			continue
		}
		path = expandHome(path, home)
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

func expandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") && home != "" {
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
}

// Load reads existing YAML mapping files in precedence order and recursively
// merges mappings. Scalars and lists from later files replace earlier values.
func Load(paths []string) (map[string]interface{}, []string, error) {
	result := make(map[string]interface{})
	sources := []string{}
	for _, path := range paths {
		file, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, sources, fmt.Errorf("open config %s: %w", path, err)
		}
		content, readErr := io.ReadAll(io.LimitReader(file, maxConfigSize+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, sources, fmt.Errorf("read config %s: %w", path, readErr)
		}
		if closeErr != nil {
			return nil, sources, fmt.Errorf("close config %s: %w", path, closeErr)
		}
		if len(content) > maxConfigSize {
			return nil, sources, fmt.Errorf("config %s exceeds 1 MiB", path)
		}
		var value interface{}
		if err := yaml.Unmarshal(content, &value); err != nil {
			return nil, sources, fmt.Errorf("parse config %s: %w", path, err)
		}
		mapping, ok := value.(map[string]interface{})
		if !ok && value != nil {
			return nil, sources, fmt.Errorf("config %s must contain a YAML mapping", path)
		}
		merge(result, mapping)
		sources = append(sources, path)
	}
	return result, sources, nil
}

func merge(target, incoming map[string]interface{}) {
	for key, value := range incoming {
		incomingMap, incomingIsMap := value.(map[string]interface{})
		targetMap, targetIsMap := target[key].(map[string]interface{})
		if incomingIsMap && targetIsMap {
			merge(targetMap, incomingMap)
			continue
		}
		target[key] = value
	}
}
