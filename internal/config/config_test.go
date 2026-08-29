package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, directory, name, contents string) string {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMergesMappingsAndReplacesScalars(t *testing.T) {
	root := t.TempDir()
	system := writeConfig(t, root, "system.cfg", "database: /system/db\ndefaults:\n  build:\n    compress: gzip\n    strip: true\n")
	user := writeConfig(t, root, "user.cfg", "database: /user/db\ndefaults:\n  build:\n    compress: zstd\n")

	loaded, sources, err := Load([]string{system, user})
	if err != nil {
		t.Fatal(err)
	}
	if loaded["database"] != "/user/db" {
		t.Fatalf("database = %#v", loaded["database"])
	}
	build := loaded["defaults"].(map[string]interface{})["build"].(map[string]interface{})
	if build["compress"] != "zstd" || build["strip"] != true {
		t.Fatalf("merged build defaults = %#v", build)
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %#v", sources)
	}
}

func TestLoadSkipsMissingFilesAndRejectsNonMapping(t *testing.T) {
	root := t.TempDir()
	invalid := writeConfig(t, root, "invalid.cfg", "- one\n- two\n")
	if _, _, err := Load([]string{filepath.Join(root, "missing.cfg"), invalid}); err == nil {
		t.Fatal("expected non-mapping configuration to fail")
	}
}

func TestCandidatePathsUseSystemThenUserThenOverrides(t *testing.T) {
	home := t.TempDir()
	paths := CandidatePaths("adpm.cfg", home, "/environment.cfg", "/explicit.cfg")
	want := []string{"/etc/adpm/adpm.cfg", filepath.Join(home, ".adpm", "adpm.cfg"), "/environment.cfg", "/explicit.cfg"}
	for index := range want {
		if paths[index] != want[index] {
			t.Fatalf("paths[%d] = %q, want %q", index, paths[index], want[index])
		}
	}
}
