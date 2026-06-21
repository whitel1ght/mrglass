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
	"strings"
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
	Ref string
	// LocalContext is true when the review ran inside a checkout of the project
	// (full repo context: CLAUDE.md, skills, all files); false for diff-only.
	LocalContext bool
	Text         string
	Err          error
}

// Reviewer turns an MR diff into review text via Claude. dir is the working
// directory to run Claude in (the project worktree for full context, or "" for
// a diff-only review with no project on disk).
type Reviewer interface {
	Review(mr core.MR, diff, prompt, dir string) Result
}

// Options configures Generate's local-context behavior.
type Options struct {
	ProjectsDir  string
	ProjectPaths map[string]string
	Worktree     Worktree // how to make an isolated checkout; nil -> diff-only
}

// Generate fetches the MR diff and asks the reviewer to produce review text. If
// a local clone is found (per opts), it runs Claude inside a throwaway worktree
// of the MR branch so Claude has full project context; otherwise it falls back
// to a diff-only review. It never posts — posting is a separate, confirmed step.
func Generate(gl GitLab, rv Reviewer, mr core.MR, prompt string, opts Options) Result {
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

	dir := ""
	local := false
	if opts.Worktree != nil {
		if repoDir, ok := ResolveDir(mr, opts.ProjectsDir, opts.ProjectPaths); ok {
			wt, cleanup, err := opts.Worktree.Prepare(repoDir, mr.IID)
			if err != nil {
				// Couldn't prepare the worktree — degrade to diff-only rather
				// than failing the whole review.
				dir = ""
			} else {
				defer cleanup()
				dir, local = wt, true
			}
		}
	}

	res := rv.Review(mr, diff, prompt, dir)
	res.LocalContext = local && res.Err == nil
	if res.Err != nil {
		// The status bar truncates; record the full error + mode for diagnosis.
		mode := "diff-only"
		if local {
			mode = "local-context dir=" + dir
		}
		logf("review %s (%s) FAILED: %v", mr.Ref, mode, res.Err)
	}
	return res
}

// Post writes the (confirmed) review text as a comment on the MR.
func Post(gl GitLab, mr core.MR, body string) error {
	return gl.PostNote(mr.ProjectID, mr.IID, body)
}

// CmdRunner runs the claude CLI with stdin, a working directory (empty = current),
// and args. Fakeable in tests.
type CmdRunner interface {
	Run(stdin, dir string, args ...string) ([]byte, error)
}

// ExecCmdRunner runs the real claude binary with a generous timeout (an agentic
// review takes longer than a triage).
type ExecCmdRunner struct{}

func (ExecCmdRunner) Run(stdin, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Stdin = bytes.NewBufferString(stdin)
	if dir != "" {
		cmd.Dir = dir // run inside the project worktree for full context
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	// Return stdout REGARDLESS of exit code: claude often exits non-zero while
	// still printing a JSON result with a useful is_error message (e.g. "Not
	// logged in"). The caller parses stdout first; only if that yields nothing
	// do we fall back to this error (with stderr folded in so it isn't lost).
	if err != nil && stderr.Len() > 0 {
		err = fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), err
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

func (cr ClaudeReviewer) Review(mr core.MR, diff, prompt, dir string) Result {
	full := fmt.Sprintf("%s\n\nMerge request: %s — %q\n\nDIFF:\n%s",
		prompt, mr.Ref, mr.Title, diff)
	args := []string{
		"-p", full,
		"--output-format", "json",
		"--allowedTools", "Read",
	}
	out, runErr := cr.R.Run(full, dir, args...)
	// Parse stdout FIRST even on a non-zero exit: claude commonly exits 1 while
	// still emitting a JSON result whose is_error message ("Not logged in", a
	// quota error, …) is far more useful than the bare exit code.
	text, validJSON, perr := parseResult(out)
	if perr == nil {
		return Result{Ref: mr.Ref, Text: text}
	}
	// If stdout was valid JSON, its message (is_error / no-result) is the real
	// reason — prefer it over the opaque process exit code. Only when stdout is
	// unusable do we fall back to the process error (which carries stderr).
	if validJSON {
		return Result{Ref: mr.Ref, Err: perr}
	}
	if runErr != nil {
		return Result{Ref: mr.Ref, Err: runErr}
	}
	return Result{Ref: mr.Ref, Err: perr}
}

// parseResult parses claude's --output-format json envelope. The bool reports
// whether the payload was valid JSON at all (vs. garbage/empty) — the caller
// uses it to decide whether a JSON is_error message should win over a non-zero
// process exit code.
func parseResult(raw []byte) (text string, validJSON bool, err error) {
	var env struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if e := json.Unmarshal(raw, &env); e != nil {
		return "", false, e
	}
	// Claude reports failures (not logged in, rate limited, refusals) via
	// is_error with the message in result. Never treat those as a review —
	// otherwise an error string could be posted to the MR as a comment.
	if env.IsError {
		msg := env.Result
		if msg == "" {
			msg = "claude reported an error"
		}
		return "", true, fmt.Errorf("claude: %s", msg)
	}
	if env.Result == "" {
		return "", true, fmt.Errorf("claude returned no result")
	}
	return env.Result, true, nil
}
