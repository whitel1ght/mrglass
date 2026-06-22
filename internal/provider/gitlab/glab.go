package gitlab

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Runner executes the glab CLI. Abstracted so tests can fake it.
type Runner interface {
	Run(args ...string) ([]byte, error)
}

// ExecRunner runs the real glab binary with a per-call timeout.
type ExecRunner struct{}

func (ExecRunner) Run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "glab", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// glab puts the real failure (auth, 4xx, validation) on stderr; without
		// this the caller only sees an opaque "exit status N".
		if s := strings.TrimSpace(stderr.String()); s != "" {
			err = fmt.Errorf("%v: %s", err, s)
		}
	}
	return stdout.Bytes(), err
}

// APIGet runs `glab api <path>`, retrying transient transport failures.
func APIGet(r Runner, path string, retries int) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		out, err := r.Run("api", path)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if !isTransient(err) {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
	}
	return nil, lastErr
}

// APIPost runs `glab api -X POST <path>` with the given form fields. Used for
// the few write operations mrglass performs (e.g. posting an MR note). Writes
// are NOT retried — a transient-looking failure could have actually succeeded,
// and we never want to post a duplicate comment.
func APIPost(r Runner, path string, fields map[string]string) ([]byte, error) {
	args := []string{"api", "-X", "POST", path}
	for k, v := range fields {
		args = append(args, "-f", k+"="+v)
	}
	return r.Run(args...)
}

func isTransient(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "eof") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "connection")
}
