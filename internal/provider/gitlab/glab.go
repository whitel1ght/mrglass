package gitlab

import (
	"context"
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
	return cmd.Output()
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

func isTransient(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "eof") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "connection")
}
