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
