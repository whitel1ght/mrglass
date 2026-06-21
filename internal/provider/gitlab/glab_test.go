package gitlab

import (
	"errors"
	"testing"
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
