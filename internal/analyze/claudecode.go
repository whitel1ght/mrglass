package analyze

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/dmitry/mrglass/internal/core"
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
	out, err := cc.R.Run("",
		"-p", prompt,
		"--output-format", "json",
		"--allowedTools", "Read",
		"--bare",
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
		Result string `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", err
	}
	if env.Result == "" {
		return "", fmt.Errorf("claude returned no result")
	}
	return env.Result, nil
}
