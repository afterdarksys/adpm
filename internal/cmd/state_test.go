package cmd

import (
	"path/filepath"
	"testing"
)

func TestStateDBPrecedence(t *testing.T) {
	t.Setenv("ADPM_DB", filepath.Join(t.TempDir(), "environment"))
	want := filepath.Join(t.TempDir(), "flag")
	got, err := stateDB(want)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("stateDB() = %q, want %q", got, want)
	}
}

func TestStateDBUsesEnvironment(t *testing.T) {
	want := filepath.Join(t.TempDir(), "state")
	t.Setenv("ADPM_DB", want)
	got, err := stateDB("")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("stateDB() = %q, want %q", got, want)
	}
}
