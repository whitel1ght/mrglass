package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultHasSections(t *testing.T) {
	c := Default()
	if len(c.Sections) == 0 {
		t.Error("default config should ship sections")
	}
	if c.Days <= 0 || c.TicketRegex == "" {
		t.Error("default Days/TicketRegex must be set")
	}
}

func TestLoadMissingReturnsDefaultsWithWarning(t *testing.T) {
	c, warns := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if len(c.Sections) == 0 {
		t.Error("missing config should fall back to defaults")
	}
	if len(warns) == 0 {
		t.Error("missing config should produce a warning")
	}
}

func TestLoadOverlaysValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("days: 7\nrefreshMinutes: 10\nautoTriage: false\n"), 0o644)
	c, warns := Load(path)
	if len(warns) != 0 {
		t.Errorf("valid config should have no warnings, got %v", warns)
	}
	if c.Days != 7 || c.RefreshMinutes != 10 || c.AutoTriage {
		t.Errorf("overlay failed: %+v", c)
	}
	// untouched fields keep defaults
	if c.TicketRegex == "" {
		t.Error("TicketRegex should retain default")
	}
}

func TestLoadBrokenYAMLDegrades(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("days: [not an int\n"), 0o644)
	c, warns := Load(path)
	if len(c.Sections) == 0 {
		t.Error("broken config should still return usable defaults")
	}
	if len(warns) == 0 {
		t.Error("broken config should warn")
	}
}
