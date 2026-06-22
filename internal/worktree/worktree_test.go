package worktree

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dmitry/mrglass/internal/core"
)

func TestFetchRef(t *testing.T) {
	gl, _ := FetchRef("gitlab", core.MR{IID: 288})
	if gl != "merge-requests/288/head" {
		t.Errorf("gitlab ref = %q", gl)
	}
	gh, _ := FetchRef("github", core.MR{Ref: "owner/repo#123"})
	if gh != "pull/123/head" {
		t.Errorf("github ref = %q", gh)
	}
	if _, err := FetchRef("github", core.MR{Ref: "no-number"}); err == nil {
		t.Error("github ref without # should error")
	}
}

func TestSlug(t *testing.T) {
	cases := []struct {
		mr   core.MR
		want string
	}{
		{core.MR{Ref: "ecfx/infra/ecfx-k8s!288", TicketKey: "ECFX-1234"}, "ecfx-k8s-ECFX-1234"},
		{core.MR{Ref: "ecfx/ecfx-k8s!288", TicketKey: "Other", IID: 288}, "ecfx-k8s-288"},
		{core.MR{Ref: "owner/repo#42", TicketKey: "", IID: 42}, "repo-42"},
	}
	for _, c := range cases {
		if got := Slug(c.mr); got != c.want {
			t.Errorf("Slug(%q) = %q, want %q", c.mr.Ref, got, c.want)
		}
	}
}

type fakeGit struct {
	listOut string // worktree list output
	calls   [][]string
	failOn  string // substring; if a call contains it, return an error
}

func (f *fakeGit) Run(repoDir string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	joined := strings.Join(args, " ")
	if f.failOn != "" && strings.Contains(joined, f.failOn) {
		return []byte("git boom"), errors.New("exit 1")
	}
	if len(args) >= 2 && args[0] == "worktree" && args[1] == "list" {
		return []byte(f.listOut), nil
	}
	return []byte(""), nil
}

func TestPrepareFetchesAndAdds(t *testing.T) {
	f := &fakeGit{}
	dir, err := Git{R: f}.Prepare("/clones/repo", "you/branch", "merge-requests/7/head", "", "repo-X-7")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dir, ".mrglass-worktrees/repo-X-7") {
		t.Errorf("dir = %q", dir)
	}
	all := ""
	for _, c := range f.calls {
		all += strings.Join(c, " ") + "\n"
	}
	if !strings.Contains(all, "fetch origin merge-requests/7/head:mrglass/repo-X-7") {
		t.Errorf("missing fetch call:\n%s", all)
	}
	if !strings.Contains(all, "worktree add -B mrglass/repo-X-7") {
		t.Errorf("missing worktree add:\n%s", all)
	}
}

func TestPrepareIdempotentReuse(t *testing.T) {
	// worktree already listed → reuse, no fetch/add
	f := &fakeGit{listOut: "worktree /clones/.mrglass-worktrees/repo-X-7\nHEAD abc\n"}
	dir, err := Git{R: f}.Prepare("/clones/repo", "b", "merge-requests/7/head", "/clones/.mrglass-worktrees", "repo-X-7")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/clones/.mrglass-worktrees/repo-X-7" {
		t.Errorf("dir = %q", dir)
	}
	for _, c := range f.calls {
		if c[0] == "fetch" {
			t.Error("should not fetch when the worktree already exists")
		}
	}
}

func TestPrepareFetchErrorSurfaces(t *testing.T) {
	f := &fakeGit{failOn: "fetch"}
	if _, err := (Git{R: f}).Prepare("/r", "b", "merge-requests/1/head", "", "s"); err == nil {
		t.Error("fetch failure should surface")
	}
}

// --- launcher ---

func TestBuildArgsSubstitutes(t *testing.T) {
	argv, err := BuildArgs("tmux new-window -c {dir} {cmd}", "/wt/x", "claude", "you/b", "ECFX-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tmux", "new-window", "-c", "/wt/x", "claude"}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %v, want %v", argv, want)
	}
}

func TestBuildArgsKeepsQuotedCmdTogether(t *testing.T) {
	// workCmd with spaces, quoted in the template, must stay one arg
	argv, err := BuildArgs(`tmux new-window -c {dir} {cmd}`, "/wt", `claude "address {key}"`, "b", "ECFX-9")
	if err != nil {
		t.Fatal(err)
	}
	// {cmd} expands to: claude "address ECFX-9"  → tokens: claude, address ECFX-9
	last := argv[len(argv)-2:] // "claude", "address ECFX-9"
	if last[0] != "claude" || last[1] != "address ECFX-9" {
		t.Errorf("quoted cmd not preserved: %v", argv)
	}
}

func TestBuildArgsEmptyTemplate(t *testing.T) {
	if _, err := BuildArgs("", "/d", "c", "b", "k"); err == nil {
		t.Error("empty template should error")
	}
}

type fakeRun struct{ argv []string }

func (f *fakeRun) Start(argv []string) error { f.argv = argv; return nil }

func TestLaunchRunsArgv(t *testing.T) {
	fr := &fakeRun{}
	if err := Launch(fr, "tmux new-window -c {dir} {cmd}", "/wt/x", "claude", "b", "k"); err != nil {
		t.Fatal(err)
	}
	if len(fr.argv) == 0 || fr.argv[0] != "tmux" {
		t.Errorf("launched argv = %v", fr.argv)
	}
}

func TestBuildArgsSessionPlaceholder(t *testing.T) {
	// {session} resolves (empty outside tmux, real name inside) and substitutes.
	argv, err := BuildArgs("tmux new-window -t {session}: -c {dir} {cmd}", "/wt", "claude", "b", "k")
	if err != nil {
		t.Fatal(err)
	}
	// the -t arg should be "<session>:" — empty session yields ":" which tmux
	// treats as the current session; non-empty yields "name:".
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "new-window -t") || !strings.Contains(joined, "-c /wt claude") {
		t.Errorf("session template not built right: %v", argv)
	}
}
