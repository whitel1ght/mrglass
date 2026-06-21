package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dmitry/mrglass/internal/analyze"
	"github.com/dmitry/mrglass/internal/config"
	"github.com/dmitry/mrglass/internal/provider/gitlab"
	"github.com/dmitry/mrglass/internal/tui"
)

var version = "dev"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		configPath  = flag.String("config", defaultConfigPath(), "path to config.yaml")
		statePath   = flag.String("state", ".mrglass-state.json", "path to snapshot state file")
		noTriage    = flag.Bool("no-triage", false, "disable Claude triage entirely")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("mrglass %s\n", version)
		return
	}

	cfg, warns := config.Load(*configPath)
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, "config:", w)
	}

	p := gitlab.New()
	me, err := p.Whoami()
	if err != nil || me == "" {
		fmt.Fprintln(os.Stderr,
			"Could not determine GitLab user — is `glab` installed and authenticated?\n"+
				"Run `glab auth status`, then `glab auth login` if needed.")
		os.Exit(1)
	}

	var az analyze.Analyzer
	if !*noTriage && analyze.Available() {
		cc := analyze.NewClaudeCode()
		az = cc
	}

	m := tui.New(cfg, p, me, az, *statePath)
	prog := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func defaultConfigPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "mrglass", "config.yaml")
	}
	return "config.yaml"
}
