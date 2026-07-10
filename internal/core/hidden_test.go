package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHiddenRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hidden.json")
	set := map[string]bool{"g/p!1": true, "g/p!2": true}
	if err := SaveHidden(path, set); err != nil {
		t.Fatal(err)
	}
	got, err := LoadHidden(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got["g/p!1"] || !got["g/p!2"] {
		t.Errorf("round-trip lost refs: %v", got)
	}
}

func TestLoadHiddenMissingFileIsEmpty(t *testing.T) {
	got, err := LoadHidden(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file must not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty set, got %v", got)
	}
}

func TestLoadHiddenCorruptFileEmptyPlusError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hidden.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadHidden(path)
	if err == nil {
		t.Error("corrupt file should return an error")
	}
	if got == nil || len(got) != 0 {
		t.Errorf("corrupt file should still return a usable empty set, got %v", got)
	}
}

func TestHiddenPath(t *testing.T) {
	if got := HiddenPath(".mrglass-state.json"); got != ".mrglass-state-hidden.json" {
		t.Errorf("got %q", got)
	}
	if got := HiddenPath("/x/state"); got != "/x/state-hidden" {
		t.Errorf("no-extension path: got %q", got)
	}
}
