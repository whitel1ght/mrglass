package review

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dmitry/mrglass/internal/core"
)

// ProjectPath extracts the "group/repo" portion from an MR ref ("group/repo!12").
func ProjectPath(ref string) string {
	if i := strings.LastIndex(ref, "!"); i >= 0 {
		return ref[:i]
	}
	return ref
}

// expandHome expands a leading "~" to the user's home directory.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

// ResolveDir finds the local clone for an MR. Resolution order:
//  1. an explicit override keyed by the MR's "group/repo" project path;
//  2. <projectsDir>/<repo-name> (the segment after the last "/");
//  3. <projectsDir>/<group/repo> (the full nested path).
//
// It returns the first candidate that exists and is a git repository. ok=false
// means no local clone was found (caller falls back to a diff-only review).
func ResolveDir(mr core.MR, projectsDir string, overrides map[string]string) (string, bool) {
	proj := ProjectPath(mr.Ref)

	if overrides != nil {
		if d, ok := overrides[proj]; ok {
			d = expandHome(d)
			if isGitRepo(d) {
				return d, true
			}
		}
	}

	if projectsDir == "" {
		return "", false
	}
	base := expandHome(projectsDir)
	name := proj
	if i := strings.LastIndex(proj, "/"); i >= 0 {
		name = proj[i+1:]
	}
	for _, cand := range []string{filepath.Join(base, name), filepath.Join(base, proj)} {
		if isGitRepo(cand) {
			return cand, true
		}
	}
	return "", false
}

func isGitRepo(dir string) bool {
	if dir == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular()) // .git dir or gitfile (worktree/submodule)
}

// Worktree prepares an isolated checkout of an MR's branch for review.
type Worktree interface {
	// Prepare fetches the MR's head and adds a detached worktree for it. It
	// returns the worktree path and a cleanup func that removes it. The caller
	// MUST defer cleanup. The main working copy is never modified.
	Prepare(repoDir string, iid int) (dir string, cleanup func(), err error)
}

// GitWorktree implements Worktree via the git CLI.
type GitWorktree struct{}

func (GitWorktree) Prepare(repoDir string, iid int) (string, func(), error) {
	noop := func() {}
	// Fetch the MR head ref — works for fork MRs too, never creates a branch.
	ref := fmt.Sprintf("merge-requests/%d/head", iid)
	if out, err := runGit(repoDir, "fetch", "origin", ref); err != nil {
		return "", noop, fmt.Errorf("fetch %s: %v: %s", ref, err, out)
	}
	wt, err := os.MkdirTemp("", fmt.Sprintf("mrglass-mr%d-", iid))
	if err != nil {
		return "", noop, err
	}
	if out, err := runGit(repoDir, "worktree", "add", "--detach", wt, "FETCH_HEAD"); err != nil {
		os.RemoveAll(wt)
		return "", noop, fmt.Errorf("worktree add: %v: %s", err, out)
	}
	cleanup := func() {
		_, _ = runGit(repoDir, "worktree", "remove", "--force", wt)
		_, _ = runGit(repoDir, "worktree", "prune")
		os.RemoveAll(wt) // belt and suspenders
	}
	return wt, cleanup, nil
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
