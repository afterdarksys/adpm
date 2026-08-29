package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/afterdarksys/adpm/internal/pkgarchive"
)

func validationFixture(t *testing.T, metadata string) string {
	t.Helper()
	stage := t.TempDir()
	if err := os.WriteFile(filepath.Join(stage, "META.json"), []byte(metadata), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "INSTALL.sh"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "fixture.adpm")
	if err := pkgarchive.WriteCPIO(stage, archive, "gzip"); err != nil {
		t.Fatal(err)
	}
	return archive
}

func TestValidateRejectsUnresolvedNativeDependencies(t *testing.T) {
	archive := validationFixture(t, `{"name":"fixture","version":"1.0.0","native_dependencies":{"unresolved":["libmissing.so"]}}`)
	_, _, err := validatePackage(archive)
	if err == nil || !strings.Contains(err.Error(), "libmissing.so") {
		t.Fatalf("expected unresolved dependency error, got %v", err)
	}
}

func TestValidateAcceptsResolvedNativeDependencies(t *testing.T) {
	archive := validationFixture(t, `{"name":"fixture","version":"1.0.0","native_dependencies":{"unresolved":[],"relocated":true}}`)
	if _, _, err := validatePackage(archive); err != nil {
		t.Fatal(err)
	}
}
