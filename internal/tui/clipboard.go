package tui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type copyResultMsg struct{ err error }

// clipboardRun pipes text into the platform clipboard tool. A package var so
// tests can fake it (no real clipboard mutation in CI).
var clipboardRun = func(text string) error {
	args, err := clipboardArgs()
	if err != nil {
		return err
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// clipboardArgs picks the platform clipboard command: pbcopy (macOS),
// clip (Windows), or the first of wl-copy/xclip/xsel found on Linux.
func clipboardArgs() ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		return []string{"pbcopy"}, nil
	case "windows":
		return []string{"clip"}, nil
	default:
		for _, tool := range []string{"wl-copy", "xclip", "xsel"} {
			if _, err := exec.LookPath(tool); err != nil {
				continue
			}
			switch tool {
			case "xclip":
				return []string{"xclip", "-selection", "clipboard"}, nil
			case "xsel":
				return []string{"xsel", "--clipboard", "--input"}, nil
			default:
				return []string{tool}, nil
			}
		}
		return nil, fmt.Errorf("no clipboard tool found (install wl-copy, xclip, or xsel)")
	}
}

// copyCmd copies text to the clipboard off the UI loop.
func copyCmd(text string) tea.Cmd {
	return func() tea.Msg { return copyResultMsg{err: clipboardRun(text)} }
}
