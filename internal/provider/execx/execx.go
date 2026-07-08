// Package execx runs a forge CLI (glab/gh) with a per-call timeout, folding
// stderr into errors, and retries transient transport failures for reads.
// Writes must never go through Retry — a transient-looking failure could have
// actually succeeded, and we never want a duplicate comment.
package execx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Runner executes a CLI. Abstracted so tests can fake it (no network).
type Runner interface {
	Run(args ...string) ([]byte, error)
}

// Exec runs the real binary with a per-call timeout.
type Exec struct{ Bin string }

func (e Exec) Run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.Bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// The CLI puts the real failure (auth, 4xx, validation) on stderr;
		// without this the caller only sees an opaque "exit status N".
		if s := strings.TrimSpace(stderr.String()); s != "" {
			err = fmt.Errorf("%v: %s", err, s)
		}
	}
	return stdout.Bytes(), err
}

// Sleep is time.Sleep, overridable in tests to avoid real backoff waits.
var Sleep = time.Sleep

// Retry runs r.Run(args...), retrying transient transport failures with linear
// backoff. READS ONLY — never route a write through this.
func Retry(r Runner, retries int, args ...string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		out, err := r.Run(args...)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if !IsTransient(err) {
			break
		}
		Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
	}
	return nil, lastErr
}

// IsTransient classifies an error as a retryable transport failure.
func IsTransient(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "eof") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "connection")
}
