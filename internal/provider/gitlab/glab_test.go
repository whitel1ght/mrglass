package gitlab

import (
	"errors"
	"strings"
	"testing"

	"github.com/dmitry/mrglass/internal/core"
)

type fakeRunner struct {
	outs [][]byte
	errs []error
	n    int
	args [][]string
}

func (f *fakeRunner) Run(args ...string) ([]byte, error) {
	f.args = append(f.args, args)
	i := f.n
	f.n++
	if i >= len(f.outs) {
		return nil, errors.New("no more responses")
	}
	return f.outs[i], f.errs[i]
}

func TestAPIGetPassesArgs(t *testing.T) {
	f := &fakeRunner{outs: [][]byte{[]byte(`{"ok":true}`)}, errs: []error{nil}}
	out, err := APIGet(f, "user", 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if string(out) != `{"ok":true}` {
		t.Errorf("out = %s", out)
	}
	if len(f.args) != 1 || f.args[0][0] != "api" || f.args[0][1] != "user" {
		t.Errorf("expected [api user], got %v", f.args)
	}
}

func TestAPIGetRetriesTransient(t *testing.T) {
	f := &fakeRunner{
		outs: [][]byte{nil, []byte(`{"ok":true}`)},
		errs: []error{errors.New("read: connection reset / EOF"), nil},
	}
	out, err := APIGet(f, "user", 2)
	if err != nil {
		t.Fatalf("should have retried and succeeded, got %v", err)
	}
	if string(out) != `{"ok":true}` {
		t.Errorf("out = %s", out)
	}
	if f.n != 2 {
		t.Errorf("expected 2 attempts, got %d", f.n)
	}
}

func TestAPIGetDoesNotRetryRealError(t *testing.T) {
	f := &fakeRunner{
		outs: [][]byte{nil},
		errs: []error{errors.New("404 Not Found")},
	}
	if _, err := APIGet(f, "bad", 2); err == nil {
		t.Fatal("expected error")
	}
	if f.n != 1 {
		t.Errorf("404 should not retry, got %d attempts", f.n)
	}
}

func TestMRDiffFormatsChanges(t *testing.T) {
	body := []byte(`{"changes":[{"old_path":"a.go","new_path":"a.go","diff":"@@ -1 +1 @@\n-x\n+y\n"},{"new_path":"b.go","diff":"@@ +1 @@\n+new\n"}]}`)
	f := &fakeRunner{outs: [][]byte{body}, errs: []error{nil}}
	p := &GitLabProvider{R: f}
	out, err := p.MRDiff(core.MR{ProjectID: 42, IID: 7})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, want := range []string{"--- a.go", "+y", "--- b.go", "+new"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff missing %q in:\n%s", want, out)
		}
	}
	// uses the changes endpoint with the right project/iid
	if got := strings.Join(f.args[0], " "); !strings.Contains(got, "projects/42/merge_requests/7/changes") {
		t.Errorf("wrong path: %v", f.args[0])
	}
}

func TestPostNoteArgs(t *testing.T) {
	f := &fakeRunner{outs: [][]byte{[]byte(`{"id":1}`)}, errs: []error{nil}}
	p := &GitLabProvider{R: f}
	if err := p.PostNote(core.MR{ProjectID: 42, IID: 7}, "looks good"); err != nil {
		t.Fatalf("err: %v", err)
	}
	got := strings.Join(f.args[0], " ")
	for _, want := range []string{"-X POST", "projects/42/merge_requests/7/notes", "body=looks good"} {
		if !strings.Contains(got, want) {
			t.Errorf("post args missing %q: %v", want, f.args[0])
		}
	}
}
