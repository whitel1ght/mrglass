package analyze

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/whitel1ght/mrglass/internal/core"
)

// CmdRunner runs the claude CLI with the given stdin and args. Fakeable in tests.
type CmdRunner interface {
	Run(stdin string, args ...string) ([]byte, error)
}

// ExecCmdRunner runs the real claude binary with a timeout.
type ExecCmdRunner struct{}

func (ExecCmdRunner) Run(stdin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Stdin = bytes.NewBufferString(stdin)
	return cmd.Output()
}

// ClaudeCode is an Analyzer that shells out to the Claude Code CLI in headless,
// read-only mode using the user's existing login.
type ClaudeCode struct {
	R CmdRunner
}

func NewClaudeCode() ClaudeCode { return ClaudeCode{R: ExecCmdRunner{}} }

// Available reports whether the claude binary is on PATH.
func Available() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

func (cc ClaudeCode) Triage(c core.Change) Advice {
	prompt := buildPrompt(c)
	// NOTE: no --bare here. Bare mode skips claude's plugin/config init, which
	// also skips credential loading — every call then fails with "Not logged
	// in" even when the user's interactive claude works fine.
	out, err := cc.R.Run("",
		"-p", prompt,
		"--output-format", "json",
		"--allowedTools", "Read",
	)
	if err != nil {
		return Advice{Ref: c.Ref, Err: err}
	}
	text, err := parseResult(out)
	if err != nil {
		return Advice{Ref: c.Ref, Err: err}
	}
	return Advice{Ref: c.Ref, Text: text}
}

func buildPrompt(c core.Change) string {
	return fmt.Sprintf(
		"A GitLab merge request (%s — %q) just changed: %s. "+
			"In 1-3 short lines, explain what this means and what I should do next. "+
			"Be concise and concrete; no preamble.",
		c.Ref, c.Title, c.Detail,
	)
}

func parseResult(raw []byte) (string, error) {
	var env struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", err
	}
	// Claude reports failures (e.g. "Not logged in", rate limits) via is_error
	// with the message in result — don't mistake those for real advice.
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
