package github

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Runner executes the gh CLI. Abstracted so tests can fake it (no network).
type Runner interface {
	Run(args ...string) ([]byte, error)
}

// ExecRunner runs the real gh binary with a per-call timeout.
type ExecRunner struct{}

func (ExecRunner) Run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// gh puts the real failure (auth, 4xx, validation) on stderr; without
		// folding it in, the caller only sees an opaque "exit status N".
		if s := strings.TrimSpace(stderr.String()); s != "" {
			err = fmt.Errorf("%v: %s", err, s)
		}
	}
	return stdout.Bytes(), err
}

// run is a thin helper so call sites read like the gh command they invoke.
func run(r Runner, args ...string) ([]byte, error) {
	return r.Run(args...)
}
