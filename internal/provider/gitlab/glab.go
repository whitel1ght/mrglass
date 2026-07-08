package gitlab

import (
	"github.com/whitel1ght/mrglass/internal/provider/execx"
)

// Runner executes the glab CLI. Alias of the shared execx.Runner so tests can
// fake it.
type Runner = execx.Runner

// NewRunner returns a Runner for the real glab binary (30s per-call timeout).
func NewRunner() Runner { return execx.Exec{Bin: "glab"} }

// APIGet runs `glab api <path>`, retrying transient transport failures.
func APIGet(r Runner, path string, retries int) ([]byte, error) {
	return execx.Retry(r, retries, "api", path)
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
