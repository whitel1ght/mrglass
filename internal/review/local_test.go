package review

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/whitel1ght/mrglass/internal/core"
)

func TestProjectPath(t *testing.T) {
	cases := map[string]string{
		"group/repo!12": "group/repo",
		"a/b/c!5":       "a/b/c",
		"noiid":         "noiid",
	}
	for ref, want := range cases {
		if got := ProjectPath(ref); got != want {
			t.Errorf("ProjectPath(%q)=%q want %q", ref, got, want)
		}
	}
}

func mkGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestResolveDirByName(t *testing.T) {
	base := t.TempDir()
	mkGitRepo(t, filepath.Join(base, "web"))
	mr := core.MR{Ref: "acme/web!2034"}
	got, ok := ResolveDir(mr, base, nil)
	if !ok || got != filepath.Join(base, "web") {
		t.Errorf("by-name resolve failed: got=%q ok=%v", got, ok)
	}
}

func TestResolveDirByNestedPath(t *testing.T) {
	base := t.TempDir()
	mkGitRepo(t, filepath.Join(base, "acme", "web"))
	mr := core.MR{Ref: "acme/web!1"}
	got, ok := ResolveDir(mr, base, nil)
	if !ok || got != filepath.Join(base, "acme", "web") {
		t.Errorf("nested resolve failed: got=%q ok=%v", got, ok)
	}
}

func TestResolveDirOverrideWins(t *testing.T) {
	base := t.TempDir()
	other := t.TempDir()
	mkGitRepo(t, filepath.Join(base, "repo"))    // by-name candidate exists
	mkGitRepo(t, filepath.Join(other, "custom")) // override target
	mr := core.MR{Ref: "group/repo!1"}
	got, ok := ResolveDir(mr, base, map[string]string{"group/repo": filepath.Join(other, "custom")})
	if !ok || got != filepath.Join(other, "custom") {
		t.Errorf("override should win: got=%q ok=%v", got, ok)
	}
}

func TestResolveDirNotFound(t *testing.T) {
	base := t.TempDir() // empty
	if _, ok := ResolveDir(core.MR{Ref: "x/y!1"}, base, nil); ok {
		t.Error("should not resolve when no clone exists")
	}
	// non-git directory is not accepted
	os.MkdirAll(filepath.Join(base, "y"), 0o755)
	if _, ok := ResolveDir(core.MR{Ref: "x/y!1"}, base, nil); ok {
		t.Error("a non-git dir must not count as a clone")
	}
}

// --- Generate with a worktree (local context path) ---

type fakeWorktree struct {
	dir       string
	err       error
	prepared  bool
	cleanedUp bool
}

func (f *fakeWorktree) Prepare(repoDir string, iid int) (string, func(), error) {
	f.prepared = true
	if f.err != nil {
		return "", func() {}, f.err
	}
	return f.dir, func() { f.cleanedUp = true }, nil
}

func TestGenerateUsesWorktreeWhenLocalCloneFound(t *testing.T) {
	base := t.TempDir()
	mkGitRepo(t, filepath.Join(base, "repo"))
	gl := &fakeGitLab{diff: "d"}
	rv := &fakeReviewer{result: Result{Ref: "g/repo!1", Text: "ok"}}
	wt := &fakeWorktree{dir: "/tmp/fake-wt"}
	res := Generate(gl, rv, core.MR{Ref: "g/repo!1", IID: 1}, "p", Options{
		ProjectsDir: base, Worktree: wt,
	})
	if !wt.prepared {
		t.Error("worktree should be prepared when a local clone is found")
	}
	if rv.gotDir != "/tmp/fake-wt" {
		t.Errorf("reviewer should run in the worktree dir, got %q", rv.gotDir)
	}
	if !res.LocalContext {
		t.Error("result should be marked LocalContext")
	}
	if !wt.cleanedUp {
		t.Error("worktree must be cleaned up")
	}
}

func TestGenerateFallsBackToDiffOnlyWhenNoClone(t *testing.T) {
	gl := &fakeGitLab{diff: "d"}
	rv := &fakeReviewer{result: Result{Ref: "g/repo!1", Text: "ok"}}
	wt := &fakeWorktree{dir: "/tmp/x"}
	res := Generate(gl, rv, core.MR{Ref: "g/repo!1", IID: 1}, "p", Options{
		ProjectsDir: t.TempDir(), // empty -> no clone
		Worktree:    wt,
	})
	if wt.prepared {
		t.Error("worktree must NOT be prepared when no clone exists")
	}
	if rv.gotDir != "" {
		t.Errorf("diff-only review should have empty dir, got %q", rv.gotDir)
	}
	if res.LocalContext {
		t.Error("diff-only result must not be marked LocalContext")
	}
}

func TestGenerateWorktreeErrorFallsBackToDiffOnly(t *testing.T) {
	base := t.TempDir()
	mkGitRepo(t, filepath.Join(base, "repo"))
	gl := &fakeGitLab{diff: "d"}
	rv := &fakeReviewer{result: Result{Ref: "g/repo!1", Text: "ok"}}
	wt := &fakeWorktree{err: os.ErrPermission}
	res := Generate(gl, rv, core.MR{Ref: "g/repo!1", IID: 1}, "p", Options{
		ProjectsDir: base, Worktree: wt,
	})
	if res.Err != nil {
		t.Fatalf("a worktree failure should degrade to diff-only, not fail: %v", res.Err)
	}
	if rv.gotDir != "" || res.LocalContext {
		t.Error("after worktree failure, review should be diff-only")
	}
}
