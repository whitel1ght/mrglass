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

func TestGcReposExcludesGitfileWorktrees(t *testing.T) {
	// Create a projectsDir with two subdirs: one real clone (.git is DIR),
	// one gitfile-based worktree (.git is FILE).
	projects := t.TempDir()

	// Real clone: .git is a directory.
	realRepo := filepath.Join(projects, "real-repo")
	if err := os.MkdirAll(filepath.Join(realRepo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Gitfile worktree: .git is a file (gitdir marker).
	linkedWorktree := filepath.Join(projects, "linked-worktree")
	if err := os.MkdirAll(linkedWorktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linkedWorktree, ".git"), []byte("gitdir: /elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.ProjectsDir = projects

	repos := gcRepos(cfg)
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d: %v", len(repos), repos)
	}
	if repos[0] != realRepo {
		t.Errorf("expected %q, got %q", realRepo, repos[0])
	}
}
