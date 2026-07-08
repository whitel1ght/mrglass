package execx

import (
	"errors"
	"testing"
	"time"
)

type fakeRunner struct {
	errs  []error // one per call; nil = success
	calls int
}

func (f *fakeRunner) Run(args ...string) ([]byte, error) {
	err := f.errs[f.calls]
	f.calls++
	if err != nil {
		return nil, err
	}
	return []byte("ok"), nil
}

func TestRetryRetriesTransient(t *testing.T) {
	Sleep = func(time.Duration) {}
	defer func() { Sleep = time.Sleep }()
	f := &fakeRunner{errs: []error{errors.New("unexpected EOF"), nil}}
	out, err := Retry(f, 2, "api", "user")
	if err != nil || string(out) != "ok" {
		t.Fatalf("want success after retry, got %q %v", out, err)
	}
	if f.calls != 2 {
		t.Errorf("want 2 calls, got %d", f.calls)
	}
}

func TestRetryStopsOnPermanentError(t *testing.T) {
	Sleep = func(time.Duration) {}
	defer func() { Sleep = time.Sleep }()
	f := &fakeRunner{errs: []error{errors.New("401 unauthorized"), nil}}
	if _, err := Retry(f, 2, "api", "user"); err == nil {
		t.Fatal("permanent error must not be retried into success")
	}
	if f.calls != 1 {
		t.Errorf("want 1 call, got %d", f.calls)
	}
}

func TestRetryGivesUpAfterRetries(t *testing.T) {
	Sleep = func(time.Duration) {}
	defer func() { Sleep = time.Sleep }()
	e := errors.New("connection reset")
	f := &fakeRunner{errs: []error{e, e, e}}
	if _, err := Retry(f, 2, "x"); err == nil {
		t.Fatal("want error after exhausting retries")
	}
	if f.calls != 3 {
		t.Errorf("want 3 calls (1 + 2 retries), got %d", f.calls)
	}
}

func TestIsTransient(t *testing.T) {
	cases := []struct {
		err  string
		want bool
	}{
		{"unexpected EOF", true},
		{"dial tcp: i/o timeout", true},
		{"connection refused", true},
		{"404 not found", false},
	}
	for _, c := range cases {
		if got := IsTransient(errors.New(c.err)); got != c.want {
			t.Errorf("IsTransient(%q) = %v, want %v", c.err, got, c.want)
		}
	}
}
