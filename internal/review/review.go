// Package review runs an agentic Claude review of an MR diff and, after the user
// confirms, posts the result as a comment. Claude runs READ-ONLY (it produces
// text only); the single GitLab write — posting the note — is performed by this
// package and is always gated by the caller on explicit user confirmation.
package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/dmitry/mrglass/internal/core"
)

// maxDiffChars caps how much diff we feed Claude, so a huge MR doesn't blow the
// token budget. Truncated diffs still yield a useful high-level review.
const maxDiffChars = 60000

// GitLab is the narrow forge capability the review feature needs: read the diff,
// and (post-confirmation) post a note. Implemented by the gitlab provider.
type GitLab interface {
	MRDiff(projectID, iid int) (string, error)
	PostNote(projectID, iid int, body string) error
}

// Result is the outcome of generating a review (before posting).
type Result struct {
	Ref  string
	Text string
	Err  error
}

// Reviewer turns an MR diff into review text via Claude.
type Reviewer interface {
	Review(mr core.MR, diff, prompt string) Result
}

// Generate fetches the MR diff and asks the reviewer to produce review text. It
// does NOT post anything — posting is a separate, user-confirmed step.
func Generate(gl GitLab, rv Reviewer, mr core.MR, prompt string) Result {
	diff, err := gl.MRDiff(mr.ProjectID, mr.IID)
	if err != nil {
		return Result{Ref: mr.Ref, Err: fmt.Errorf("fetch diff: %w", err)}
	}
	if diff == "" {
		return Result{Ref: mr.Ref, Err: fmt.Errorf("no diff to review")}
	}
	if len(diff) > maxDiffChars {
		diff = diff[:maxDiffChars] + "\n…(diff truncated)…"
	}
	return rv.Review(mr, diff, prompt)
}

// Post writes the (confirmed) review text as a comment on the MR.
func Post(gl GitLab, mr core.MR, body string) error {
	return gl.PostNote(mr.ProjectID, mr.IID, body)
}

// CmdRunner runs the claude CLI with stdin and args. Fakeable in tests.
type CmdRunner interface {
	Run(stdin string, args ...string) ([]byte, error)
}

// ExecCmdRunner runs the real claude binary with a generous timeout (an agentic
// review takes longer than a triage).
type ExecCmdRunner struct{}

func (ExecCmdRunner) Run(stdin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Stdin = bytes.NewBufferString(stdin)
	return cmd.Output()
}

// ClaudeReviewer reviews via the Claude Code CLI, headless and READ-ONLY.
type ClaudeReviewer struct {
	R CmdRunner
}

func NewClaudeReviewer() ClaudeReviewer { return ClaudeReviewer{R: ExecCmdRunner{}} }

// Available reports whether the claude binary is on PATH.
func Available() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

func (cr ClaudeReviewer) Review(mr core.MR, diff, prompt string) Result {
	full := fmt.Sprintf("%s\n\nMerge request: %s — %q\n\nDIFF:\n%s",
		prompt, mr.Ref, mr.Title, diff)
	out, err := cr.R.Run(full,
		"-p", full,
		"--output-format", "json",
		"--allowedTools", "Read",
		"--bare",
	)
	if err != nil {
		return Result{Ref: mr.Ref, Err: err}
	}
	text, err := parseResult(out)
	if err != nil {
		return Result{Ref: mr.Ref, Err: err}
	}
	return Result{Ref: mr.Ref, Text: text}
}

func parseResult(raw []byte) (string, error) {
	var env struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", err
	}
	// Claude reports failures (not logged in, rate limited, refusals) via
	// is_error with the message in result. Never treat those as a review —
	// otherwise an error string could be posted to the MR as a comment.
	if env.IsError {
		msg := env.Result
		if msg == "" {
			msg = "claude reported an error"
		}
		return "", fmt.Errorf("claude: %s", msg)
	}
	if env.Result == "" {
		return "", fmt.Errorf("claude returned no result")
	}
	return env.Result, nil
}
