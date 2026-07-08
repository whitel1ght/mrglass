package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/whitel1ght/mrglass/internal/config"
	"github.com/whitel1ght/mrglass/internal/provider"
	"github.com/whitel1ght/mrglass/internal/worktree"
)

// gcListDays effectively disables List's updated-after window: GC must see ALL
// the user's open MRs, or a stale-but-open MR's worktree would look removable.
const gcListDays = 3650

// runGC finds mrglass-managed worktrees whose MR is no longer open, prints the
// plan, and (unless dryRun) removes them after one y/N confirmation. Dirty
// worktrees (uncommitted or unpushed work) are reported and never removed.
func runGC(cfg config.Config, p provider.Provider, me string, dryRun bool, in io.Reader, out io.Writer) error {
	mrs, err := p.List(me, gcListDays, cfg.TicketRegex)
	if err != nil {
		return fmt.Errorf("listing open MRs: %w", err)
	}
	openSlugs := map[string]bool{}
	for _, mr := range mrs {
		openSlugs[worktree.Slug(mr)] = true
	}

	git := worktree.New()
	var remove, skip []worktree.GCItem
	for _, repo := range gcRepos(cfg) {
		items, err := worktree.ListGC(git.R, repo, expandHome(cfg.Worktree.Dir))
		if err != nil {
			fmt.Fprintf(out, "⚠ %s: %v\n", repo, err)
			continue
		}
		r, s := worktree.Removable(items, openSlugs)
		remove, skip = append(remove, r...), append(skip, s...)
	}

	for _, it := range skip {
		fmt.Fprintf(out, "skip  %s  [dirty — has local work]\n", it.Path)
	}
	if len(remove) == 0 {
		fmt.Fprintln(out, "nothing to remove")
		return nil
	}
	for _, it := range remove {
		fmt.Fprintf(out, "remove %s  (branch %s; MR no longer open)\n", it.Path, it.Branch)
	}
	if dryRun {
		return nil
	}

	fmt.Fprintf(out, "Remove %d worktree(s)? [y/N] ", len(remove))
	sc := bufio.NewScanner(in)
	if !sc.Scan() || strings.ToLower(strings.TrimSpace(sc.Text())) != "y" {
		fmt.Fprintln(out, "aborted")
		return nil
	}
	for _, it := range remove {
		if err := git.Remove(it); err != nil {
			fmt.Fprintf(out, "⚠ %v\n", err)
			continue
		}
		fmt.Fprintf(out, "✓ removed %s\n", it.Path)
	}
	return nil
}

// gcRepos lists git repos one level under projectsDir — the clones whose
// sibling .mrglass-worktrees (or worktree.dir) may hold `w` worktrees.
// Only real clones are scanned; gitfile-based linked worktrees are deliberately
// excluded (their .git is a file, not a directory).
func gcRepos(cfg config.Config) []string {
	root := expandHome(cfg.ProjectsDir)
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var repos []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil && fi.IsDir() {
			repos = append(repos, dir)
		}
	}
	return repos
}

// expandHome expands a leading ~/ to the user's home directory.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
