package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestForgeDefaultsToGitlab(t *testing.T) {
	if c := Default(); c.Forge != ForgeGitLab {
		t.Errorf("default forge = %q, want gitlab", c.Forge)
	}
	// unknown forge in a file -> falls back to gitlab with a warning
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	os.WriteFile(p, []byte("forge: bitbucket\n"), 0o644)
	c, warns := Load(p)
	if c.Forge != ForgeGitLab {
		t.Errorf("unknown forge should fall back to gitlab, got %q", c.Forge)
	}
	if len(warns) == 0 {
		t.Error("unknown forge should warn")
	}
}

func TestForgeGitHubAccepted(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	os.WriteFile(p, []byte("forge: github\n"), 0o644)
	c, _ := Load(p)
	if c.Forge != ForgeGitHub {
		t.Errorf("forge github should be accepted, got %q", c.Forge)
	}
}

func TestLegacyJiraBaseURLMigrates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	os.WriteFile(p, []byte("jira:\n  baseURL: https://ecfxdev.atlassian.net\n"), 0o644)
	c, warns := Load(p)
	want := "https://ecfxdev.atlassian.net/browse/{key}"
	if c.Tickets.URLTemplate != want {
		t.Errorf("legacy jira.baseURL should migrate to urlTemplate %q, got %q", want, c.Tickets.URLTemplate)
	}
	if c.Tickets.JiraBaseURL != "https://ecfxdev.atlassian.net" || c.Tickets.Status != "jira" {
		t.Errorf("migration should set jiraBaseURL+status, got %+v", c.Tickets)
	}
	if c.Jira.BaseURL != "" {
		t.Error("legacy Jira field should be cleared after migration")
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w, "deprecated") {
			found = true
		}
	}
	if !found {
		t.Errorf("migration should warn about deprecation, warns=%v", warns)
	}
}

func TestNewTicketsConfigNotOverriddenByMigration(t *testing.T) {
	// if tickets.urlTemplate is already set, a stray legacy jira block is ignored
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	os.WriteFile(p, []byte("tickets:\n  urlTemplate: https://linear.app/x/issue/{key}\njira:\n  baseURL: https://old\n"), 0o644)
	c, _ := Load(p)
	if c.Tickets.URLTemplate != "https://linear.app/x/issue/{key}" {
		t.Errorf("explicit tickets.urlTemplate must win, got %q", c.Tickets.URLTemplate)
	}
}
