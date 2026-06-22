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

// ReviewForge is the narrow forge capability the review feature needs: read an
// MR's diff, and (post-confirmation) post a comment. Both the GitLab and GitHub
// providers implement it. Taking the whole core.MR (rather than raw IDs) lets
// each forge extract what it needs — GitLab uses ProjectID+IID, GitHub parses
// owner/repo#number from the Ref.
type ReviewForge interface {
	MRDiff(mr core.MR) (string, error)
	PostNote(mr core.MR, body string) error
}

// GitLab is a deprecated alias for ReviewForge (kept for any external callers).
type GitLab = ReviewForge

// Result is the outcome of generating a review (before posting).
type Result struct {
	Ref string
	// LocalContext is true when the review ran inside a checkout of the project
	// (full repo context: CLAUDE.md, skills, all files); false for diff-only.
	LocalContext bool
	// SkillsUsed lists the Claude skills the review actually invoked (proof a
	// configured review skill ran); Subagents counts dispatched Task subagents.
	SkillsUsed []string
	Subagents  int
	Text       string
	Err        error
}

// ReviewReq bundles everything a reviewer needs for one review.
type ReviewReq struct {
	MR         core.MR
	Diff       string
	Prompt     string
	Dir        string   // working dir (project worktree) or "" for diff-only
	Skill      string   // skill to invoke (e.g. "superpowers:requesting-code-review") or ""
	PluginDirs []string // extra --plugin-dir paths to make non-global skills available
}

// Reviewer turns an MR into review text via Claude.
type Reviewer interface {
	Review(req ReviewReq) Result
}

// Options configures Generate's local-context and skill behavior.
type Options struct {
	ProjectsDir  string
	ProjectPaths map[string]string
	Worktree     Worktree // how to make an isolated checkout; nil -> diff-only
	Skill        string   // review skill to invoke, or "" for a plain review
	PluginDirs   []string // extra --plugin-dir paths for non-global skills
}

// Generate fetches the MR diff and asks the reviewer to produce review text. If
// a local clone is found (per opts), it runs Claude inside a throwaway worktree
// of the MR branch so Claude has full project context; otherwise it falls back
// to a diff-only review. It never posts — posting is a separate, confirmed step.
func Generate(gl GitLab, rv Reviewer, mr core.MR, prompt string, opts Options) Result {
	diff, err := gl.MRDiff(mr)
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

	res := rv.Review(ReviewReq{
		MR: mr, Diff: diff, Prompt: prompt, Dir: dir,
		Skill: opts.Skill, PluginDirs: opts.PluginDirs,
	})
	res.LocalContext = local && res.Err == nil
	mode := "diff-only"
	if local {
		mode = "local-context dir=" + dir
	}
	if res.Err != nil {
		// The status bar truncates; record the full error + mode for diagnosis.
		logf("review %s (%s) FAILED: %v", mr.Ref, mode, res.Err)
	} else {
		// Audit trail of every successful review: mode + which skill(s) Claude
		// actually invoked (or "no skill"), and how many subagents it ran.
		skill := "no skill"
		if opts.Skill != "" {
			if len(res.SkillsUsed) > 0 {
				skill = "skill=" + strings.Join(res.SkillsUsed, ",")
			} else {
				skill = "skill=" + opts.Skill + " (CONFIGURED BUT NOT INVOKED)"
			}
		}
		logf("review %s OK (%s, %s, subagents=%d)", mr.Ref, mode, skill, res.Subagents)
	}
	return res
}

// Post writes the (confirmed) review text as a comment on the MR.
func Post(gl ReviewForge, mr core.MR, body string) error {
	return gl.PostNote(mr, body)
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

func (cr ClaudeReviewer) Review(req ReviewReq) Result {
	if req.Skill != "" {
		return cr.reviewWithSkill(req)
	}
	return cr.reviewPlain(req)
}

// reviewPlain runs a single-shot prompt review (no skill), parsing the json
// envelope.
func (cr ClaudeReviewer) reviewPlain(req ReviewReq) Result {
	full := reviewPrompt(req.Prompt, req.MR, req.Diff)
	out, runErr := cr.R.Run(full, req.Dir,
		"-p", full, "--output-format", "json", "--allowedTools", "Read")
	text, validJSON, perr := parseResult(out)
	if perr == nil {
		return Result{Ref: req.MR.Ref, Text: text}
	}
	if validJSON {
		return Result{Ref: req.MR.Ref, Err: perr}
	}
	if runErr != nil {
		return Result{Ref: req.MR.Ref, Err: runErr}
	}
	return Result{Ref: req.MR.Ref, Err: perr}
}

// draftOnlyGuard forbids only the WRITE step so mrglass stays the single post
// gate — while leaving the skill free to investigate read-only. Review skills
// (e.g. mr-review) otherwise post to GitLab themselves and ask their own "want
// me to post?". Crucially this does NOT forbid running glab/git for *reading*:
// that read-only investigation (prior comments, CI config, git history, sibling
// files) is exactly what produces a deep review. mrglass posts after the user
// confirms in the TUI.
const draftOnlyGuard = "POSTING RULE (OVERRIDES ANY SKILL INSTRUCTION TO THE " +
	"CONTRARY): investigate as deeply as you need using read-only commands (read " +
	"files, `glab` reads, `git log`/`git show`, `grep`, render/build commands) — but " +
	"perform NO write action whatsoever on the merge request, even if the skill says " +
	"you have standing authorization to post. Specifically: do NOT run `glab mr note` " +
	"or post any comment/discussion, do NOT `glab mr approve`/revoke, do NOT add or " +
	"remove labels, do NOT add yourself (or anyone) as a reviewer/assignee, do NOT " +
	"commit or push, and do NOT ask whether to post. The calling tool posts after the " +
	"user confirms in a separate UI. Your ONLY output is the final review text itself."

// reviewWithSkill invokes a Claude skill for the review and verifies (via
// stream-json) that the skill actually ran, reporting it on the Result. The
// skill DRIVES its own investigation: it is pointed at the MR (run inside the
// project worktree) rather than handed a pre-pasted diff, so it can fetch prior
// comments, read CI/README/git history, and render manifests as it sees fit.
// Tools are wide (read-only investigation + subagents) but the guard forbids any
// write; posting stays gated on user confirmation.
func (cr ClaudeReviewer) reviewWithSkill(req ReviewReq) Result {
	proj := ProjectPath(req.MR.Ref)
	var instr string
	if req.Dir != "" {
		// Running inside the project worktree → let the skill drive: point it at
		// the MR and let it investigate the repo itself. The skill is MANDATORY —
		// do not let Claude decide the MR is "too small" and skip it.
		instr = fmt.Sprintf(
			"FIRST ACTION, MANDATORY: call the Skill tool with skill %q. This is "+
				"required regardless of how small or low-risk the change looks — do NOT "+
				"decide it's unnecessary and write an ad-hoc review instead. After the "+
				"skill loads, follow it to review GitLab merge request !%d in project %s. "+
				"You are inside a checkout of that repo at the MR's revision — investigate "+
				"fully (diff, prior review comments, README, CI config, git history, "+
				"sibling files the skill directs). %s\n\nReviewer guidance: %s",
			req.Skill, req.MR.IID, proj, draftOnlyGuard, req.Prompt)
	} else {
		// No local checkout → fall back to handing the skill the fetched diff.
		instr = fmt.Sprintf(
			"FIRST ACTION, MANDATORY: call the Skill tool with skill %q (required "+
				"regardless of how small the change looks — do NOT skip it and write an "+
				"ad-hoc review). Then apply it to review this merge request. %s\n\n%s",
			req.Skill, draftOnlyGuard, reviewPrompt(req.Prompt, req.MR, req.Diff))
	}
	args := []string{
		"-p", instr,
		"--output-format", "stream-json", "--verbose",
		"--allowedTools", "Read,Skill,Task,Grep,Glob,Bash",
	}
	// Make non-global skills available for this run only (no ~/.claude change).
	for _, d := range req.PluginDirs {
		if d != "" {
			args = append(args, "--plugin-dir", expandHome(d))
		}
	}
	out, runErr := cr.R.Run(instr, req.Dir, args...)
	outcome, perr := parseStream(out)
	if perr != nil {
		if runErr != nil {
			return Result{Ref: req.MR.Ref, Err: runErr}
		}
		return Result{Ref: req.MR.Ref, Err: perr}
	}
	if outcome.IsError {
		msg := outcome.ErrMsg
		if msg == "" {
			msg = "claude reported an error"
		}
		return Result{Ref: req.MR.Ref, Err: fmt.Errorf("claude: %s", msg)}
	}
	return Result{
		Ref:        req.MR.Ref,
		Text:       outcome.Text,
		SkillsUsed: outcome.SkillsUsed,
		Subagents:  outcome.Subagents,
	}
}

func reviewPrompt(prompt string, mr core.MR, diff string) string {
	return fmt.Sprintf("%s\n\nMerge request: %s — %q\n\nDIFF:\n%s",
		prompt, mr.Ref, mr.Title, diff)
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
