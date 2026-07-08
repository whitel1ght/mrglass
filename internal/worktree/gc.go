package worktree

import (
	"fmt"
	"path/filepath"
	"strings"
)

// GCItem is one mrglass-managed worktree eligible for garbage collection.
type GCItem struct {
	RepoDir string // main clone the worktree belongs to
	Path    string // worktree directory
	Branch  string // "mrglass/<slug>"
	Slug    string
	Dirty   bool // uncommitted changes or commits not on any origin ref
}

// DefaultBase is where `w` creates worktrees when worktree.dir isn't set.
func DefaultBase(repoDir string) string {
	return filepath.Join(filepath.Dir(repoDir), ".mrglass-worktrees")
}

// ListGC finds mrglass-managed worktrees of repoDir: entries of
// `git worktree list --porcelain` on an mrglass/* branch under the worktree
// base. baseDir "" means DefaultBase(repoDir). Each item is checked for
// dirtiness (uncommitted changes, or commits absent from origin) so callers
// can refuse to remove work in progress.
func ListGC(g GitRunner, repoDir, baseDir string) ([]GCItem, error) {
	if baseDir == "" {
		baseDir = DefaultBase(repoDir)
	}
	out, err := g.Run(repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("worktree list: %v", err)
	}
	var items []GCItem
	var path string
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch refs/heads/mrglass/"):
			slug := strings.TrimPrefix(line, "branch refs/heads/mrglass/")
			if !strings.HasPrefix(path, baseDir+string(filepath.Separator)) {
				continue // an mrglass/* branch checked out elsewhere: not ours to GC
			}
			items = append(items, GCItem{
				RepoDir: repoDir,
				Path:    path,
				Branch:  "mrglass/" + slug,
				Slug:    slug,
				Dirty:   isDirty(g, path),
			})
		}
	}
	return items, nil
}

// isDirty reports uncommitted changes or commits not present on any origin
// ref. Errors count as dirty — when in doubt, don't delete.
func isDirty(g GitRunner, dir string) bool {
	out, err := g.Run(dir, "status", "--porcelain")
	if err != nil || strings.TrimSpace(string(out)) != "" {
		return true
	}
	out, err = g.Run(dir, "rev-list", "--count", "HEAD", "--not", "--remotes=origin")
	if err != nil || strings.TrimSpace(string(out)) != "0" {
		return true
	}
	return false
}

// Removable splits items into removable (MR no longer open, worktree clean)
// and skipped-dirty (MR no longer open but has local work). Items whose MR is
// still open are excluded from both.
func Removable(items []GCItem, openSlugs map[string]bool) (remove, skipDirty []GCItem) {
	for _, it := range items {
		if openSlugs[it.Slug] {
			continue
		}
		if it.Dirty {
			skipDirty = append(skipDirty, it)
			continue
		}
		remove = append(remove, it)
	}
	return remove, skipDirty
}

// Remove deletes a GC'd worktree: the worktree itself, its mrglass/* branch,
// then a prune. Not forced — a clean check happened in Removable; if git still
// refuses, surface that instead of deleting work.
func (g Git) Remove(item GCItem) error {
	if out, err := g.R.Run(item.RepoDir, "worktree", "remove", item.Path); err != nil {
		return fmt.Errorf("worktree remove %s: %v: %s", item.Path, err, strings.TrimSpace(string(out)))
	}
	if out, err := g.R.Run(item.RepoDir, "branch", "-D", item.Branch); err != nil {
		return fmt.Errorf("branch -D %s: %v: %s", item.Branch, err, strings.TrimSpace(string(out)))
	}
	if _, err := g.R.Run(item.RepoDir, "worktree", "prune"); err != nil {
		return fmt.Errorf("worktree prune: %v", err)
	}
	return nil
}
