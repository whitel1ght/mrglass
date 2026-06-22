// Package worktree opens an MR's branch in a dedicated, persistent git worktree
// and launches a configurable terminal command there (the `w` hotkey). Unlike
// the review feature's throwaway detached worktree, these persist on a local
// branch so the user can edit/commit/push; the main clone is never touched.
package worktree

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dmitry/mrglass/internal/core"
)

// GitRunner runs git with args in repoDir. Fakeable in tests.
type GitRunner interface {
	Run(repoDir string, args ...string) ([]byte, error)
}

// ExecGit runs the real git binary.
type ExecGit struct{}

func (ExecGit) Run(repoDir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
	return cmd.CombinedOutput()
}

// FetchRef returns the forge-specific ref that points at an MR's head, so a
// fetch lands the MR's tip regardless of fork. GitLab: merge-requests/<iid>/head;
// GitHub: pull/<number>/head (number parsed from the "owner/repo#n" ref).
func FetchRef(forge string, mr core.MR) (string, error) {
	switch forge {
	case "github":
		if i := strings.LastIndex(mr.Ref, "#"); i >= 0 {
			return "pull/" + mr.Ref[i+1:] + "/head", nil
		}
		return "", fmt.Errorf("cannot derive PR number from ref %q", mr.Ref)
	default: // gitlab
		return fmt.Sprintf("merge-requests/%d/head", mr.IID), nil
	}
}

var slugUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Slug is a stable, filesystem-safe per-MR worktree name, e.g.
// "ecfx-k8s-ECFX-1234" (repo name + ticket key, or the MR iid when no ticket).
func Slug(mr core.MR) string {
	repo := mr.Ref
	if i := strings.IndexAny(repo, "!#"); i >= 0 {
		repo = repo[:i] // strip "!iid" / "#n"
	}
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		repo = repo[i+1:] // last path segment
	}
	id := mr.TicketKey
	if id == "" || id == "Other" {
		id = fmt.Sprintf("%d", mr.IID)
	}
	return slugUnsafe.ReplaceAllString(repo+"-"+id, "-")
}

// Worktreer prepares the worktree (interface for testing).
type Worktreer interface {
	Prepare(repoDir, branch, fetchRef, baseDir, slug string) (string, error)
}

// Git implements Worktreer via the git CLI.
type Git struct{ R GitRunner }

func New() Git { return Git{R: ExecGit{}} }

// Prepare ensures a persistent worktree for the MR branch exists and returns its
// path. Idempotent: an existing worktree for this slug is reused. baseDir ""
// defaults to <repoDir>/../.mrglass-worktrees.
func (g Git) Prepare(repoDir, branch, fetchRef, baseDir, slug string) (string, error) {
	if baseDir == "" {
		baseDir = filepath.Join(filepath.Dir(repoDir), ".mrglass-worktrees")
	}
	dir := filepath.Join(baseDir, slug)

	// Already a worktree at this path? Reuse it (idempotent re-open).
	if out, err := g.R.Run(repoDir, "worktree", "list", "--porcelain"); err == nil {
		if strings.Contains(string(out), "worktree "+dir+"\n") {
			return dir, nil
		}
	}

	// Fetch the MR head into a local branch named for the MR.
	local := "mrglass/" + slug
	if out, err := g.R.Run(repoDir, "fetch", "origin", fetchRef+":"+local); err != nil {
		return "", fmt.Errorf("fetch %s: %v: %s", fetchRef, err, strings.TrimSpace(string(out)))
	}
	// Add the worktree on that branch. -B resets the branch to the fetched tip if
	// it already exists; --force tolerates a stale checkout.
	if out, err := g.R.Run(repoDir, "worktree", "add", "-B", local, dir, local); err != nil {
		return "", fmt.Errorf("worktree add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return dir, nil
}
