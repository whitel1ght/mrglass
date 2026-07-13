package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigPathPrefersXDG(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	want := filepath.Join(tmp, "mrglass", "config.yaml")
	os.MkdirAll(filepath.Dir(want), 0o755)
	os.WriteFile(want, []byte("days: 1\n"), 0o644)
	if got := defaultConfigPath(); got != want {
		t.Errorf("got %q, want XDG path %q", got, want)
	}
}
func TestDefaultConfigPathFallsBackToCreatePath(t *testing.T) {
	// nothing exists -> returns the first (preferred) candidate to create
	t.Setenv("XDG_CONFIG_HOME", "")
	got := defaultConfigPath()
	if got == "" || filepath.Base(got) != "config.yaml" {
		t.Errorf("unexpected create-path: %q", got)
	}
}

func TestDefaultStatePathUsesXDGStateHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)
	// Run from a dir with NO legacy state file so the XDG default is used.
	t.Chdir(t.TempDir())
	want := filepath.Join(tmp, "mrglass", "state.json")
	if got := defaultStatePath(); got != want {
		t.Errorf("got %q, want XDG state path %q", got, want)
	}
}

func TestDefaultStatePathPrefersExistingLegacyCWDFile(t *testing.T) {
	// Back-compat: if a .mrglass-state.json already sits in the CWD, keep using
	// it rather than orphaning the user's existing state/hidden files.
	dir := t.TempDir()
	t.Chdir(dir)
	legacy := filepath.Join(dir, ".mrglass-state.json")
	os.WriteFile(legacy, []byte("{}"), 0o644)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if got := defaultStatePath(); got != ".mrglass-state.json" {
		t.Errorf("should reuse existing CWD state file, got %q", got)
	}
}
