package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whitel1ght/mrglass/internal/config"
	"github.com/whitel1ght/mrglass/internal/core"
)

type fakeProvider struct{ mrs []core.MR }

func (f fakeProvider) Whoami() (string, error) { return "me", nil }
func (f fakeProvider) List(me string, days int, ticketPattern string) ([]core.MR, error) {
	return f.mrs, nil
}

func TestRunGCDryRunListsWithoutPrompting(t *testing.T) {
	// A projectsDir with one "repo" (a dir containing .git).
	projects := t.TempDir()
	repo := filepath.Join(projects, "api")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.ProjectsDir = projects

	var out bytes.Buffer
	// No git worktrees exist → plan is empty; dry-run must not read stdin.
	err := runGC(cfg, fakeProvider{}, "me", true, strings.NewReader(""), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing to remove") {
		t.Errorf("empty plan should say so, got %q", out.String())
	}
}

func TestRunGCNoAnswerRemovesNothing(t *testing.T) {
	projects := t.TempDir()
	cfg := config.Default()
	cfg.ProjectsDir = projects
	var out bytes.Buffer
	if err := runGC(cfg, fakeProvider{}, "me", false, strings.NewReader("n\n"), &out); err != nil {
		t.Fatal(err)
	}
}
