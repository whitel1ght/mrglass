package worktree

import (
	"fmt"
	"os/exec"
	"strings"
)

// CmdRunner starts a detached command (argv). Fakeable in tests.
type CmdRunner interface {
	Start(argv []string) error
}

// ExecRunner starts the command detached and reaps it in the background, so the
// launcher never blocks the UI and the child's stdio doesn't touch our TTY.
type ExecRunner struct{}

func (ExecRunner) Start(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("empty command")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// BuildArgs renders the openCommand template and splits it into argv. Placeholders
// {dir} {cmd} {branch} {key} are replaced first, then the result is tokenized
// (shell-style, honoring quotes). Returns an error for an empty template or
// unbalanced quotes.
func BuildArgs(openCommand, dir, workCmd, branch, key string) ([]string, error) {
	if strings.TrimSpace(openCommand) == "" {
		return nil, fmt.Errorf("empty openCommand")
	}
	// Expand {dir}/{branch}/{key} inside workCmd first, so a workCmd like
	// `claude "address {key}"` gets its own placeholders filled before it is
	// substituted as {cmd} and the whole thing is tokenized.
	vals := strings.NewReplacer("{dir}", dir, "{branch}", branch, "{key}", key)
	cmd := vals.Replace(workCmd)
	full := strings.NewReplacer(
		"{dir}", dir,
		"{cmd}", cmd,
		"{branch}", branch,
		"{key}", key,
	).Replace(openCommand)
	argv, err := tokenize(full)
	if err != nil {
		return nil, err
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("openCommand produced no command")
	}
	return argv, nil
}

// Launch renders the template and starts the command detached.
func Launch(run CmdRunner, openCommand, dir, workCmd, branch, key string) error {
	argv, err := BuildArgs(openCommand, dir, workCmd, branch, key)
	if err != nil {
		return err
	}
	return run.Start(argv)
}

// tokenize splits a command string into argv, honoring single/double quotes so a
// substituted value containing spaces (e.g. claude "do the thing") stays one arg.
func tokenize(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inWord := false
	var quote rune // 0, '\'' or '"'
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
			inWord = true
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				args = append(args, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unbalanced quote in command")
	}
	if inWord {
		args = append(args, cur.String())
	}
	return args, nil
}
