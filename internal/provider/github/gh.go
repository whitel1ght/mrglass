package github

import (
	"github.com/whitel1ght/mrglass/internal/provider/execx"
)

// Runner executes the gh CLI. Alias of the shared execx.Runner so tests can
// fake it (no network).
type Runner = execx.Runner

// NewRunner runs the real gh binary with a per-call timeout.
func NewRunner() Runner { return execx.Exec{Bin: "gh"} }

// run executes a READ command, retrying transient transport failures (parity
// with the GitLab provider). Writes (pr comment) call r.Run directly.
func run(r Runner, args ...string) ([]byte, error) {
	return execx.Retry(r, 2, args...)
}
