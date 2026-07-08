package worktree

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// gcGit fakes GitRunner keyed by the joined args (worktree_test.go has a
// similar fake for Prepare; this one also varies by repoDir/dir).
type gcGit struct {
	out   map[string]string // "<dir>|<args joined>" -> stdout
	calls []string
}

func (g *gcGit) Run(dir string, args ...string) ([]byte, error) {
	key := dir + "|" + strings.Join(args, " ")
	g.calls = append(g.calls, key)
	if v, ok := g.out[key]; ok {
		return []byte(v), nil
	}
	return nil, errors.New("unexpected git call: " + key)
}

func TestListGCFindsMrglassWorktrees(t *testing.T) {
	repo := "/p/api"
	base := DefaultBase(repo) // /p/.mrglass-worktrees
	wt := filepath.Join(base, "api-PROJ-1")
	porcelain := "worktree " + repo + "\nHEAD aaa\nbranch refs/heads/main\n\n" +
		"worktree " + wt + "\nHEAD bbb\nbranch refs/heads/mrglass/api-PROJ-1\n\n"
	g := &gcGit{out: map[string]string{
		repo + "|worktree list --porcelain":                  porcelain,
		wt + "|status --porcelain":                           "",
		wt + "|rev-list --count HEAD --not --remotes=origin": "0\n",
	}}
	items, err := ListGC(g, repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 mrglass worktree (main checkout excluded), got %v", items)
	}
	it := items[0]
	if it.Slug != "api-PROJ-1" || it.Branch != "mrglass/api-PROJ-1" || it.Path != wt || it.Dirty {
		t.Errorf("bad item: %+v", it)
	}
}

func TestListGCMarksDirty(t *testing.T) {
	repo := "/p/api"
	base := DefaultBase(repo)
	wt := filepath.Join(base, "api-2")
	porcelain := "worktree " + wt + "\nHEAD bbb\nbranch refs/heads/mrglass/api-2\n\n"
	g := &gcGit{out: map[string]string{
		repo + "|worktree list --porcelain": porcelain,
		wt + "|status --porcelain":          " M main.go\n",
	}}
	items, err := ListGC(g, repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Dirty {
		t.Errorf("uncommitted changes should mark dirty: %+v", items)
	}
}

func TestListGCMarksUnpushedDirty(t *testing.T) {
	repo := "/p/api"
	base := DefaultBase(repo)
	wt := filepath.Join(base, "api-3")
	porcelain := "worktree " + wt + "\nHEAD ccc\nbranch refs/heads/mrglass/api-3\n\n"
	g := &gcGit{out: map[string]string{
		repo + "|worktree list --porcelain":                  porcelain,
		wt + "|status --porcelain":                           "",
		wt + "|rev-list --count HEAD --not --remotes=origin": "2\n",
	}}
	items, err := ListGC(g, repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Dirty {
		t.Errorf("unpushed commits should mark dirty: %+v", items)
	}
}

func TestRemovable(t *testing.T) {
	items := []GCItem{
		{Slug: "api-OPEN-1"},              // MR still open → keep, not listed
		{Slug: "api-GONE-2"},              // MR gone, clean → remove
		{Slug: "api-GONE-3", Dirty: true}, // MR gone, dirty → skip (reported)
	}
	open := map[string]bool{"api-OPEN-1": true}
	remove, skip := Removable(items, open)
	if len(remove) != 1 || remove[0].Slug != "api-GONE-2" {
		t.Errorf("remove = %+v", remove)
	}
	if len(skip) != 1 || skip[0].Slug != "api-GONE-3" {
		t.Errorf("skip = %+v", skip)
	}
}

func TestRemoveRunsGitCleanup(t *testing.T) {
	g := &gcGit{out: map[string]string{
		"/p/api|worktree remove /p/.mrglass-worktrees/api-2": "",
		"/p/api|branch -D mrglass/api-2":                     "",
		"/p/api|worktree prune":                              "",
	}}
	git := Git{R: g}
	item := GCItem{RepoDir: "/p/api", Path: "/p/.mrglass-worktrees/api-2", Branch: "mrglass/api-2", Slug: "api-2"}
	if err := git.Remove(item); err != nil {
		t.Fatal(err)
	}
	if len(g.calls) != 3 {
		t.Errorf("want 3 git calls, got %v", g.calls)
	}
}
