package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whitel1ght/mrglass/internal/analyze"
	"github.com/whitel1ght/mrglass/internal/config"
	"github.com/whitel1ght/mrglass/internal/jira"
	"github.com/whitel1ght/mrglass/internal/provider"
	"github.com/whitel1ght/mrglass/internal/provider/github"
	"github.com/whitel1ght/mrglass/internal/provider/gitlab"
	"github.com/whitel1ght/mrglass/internal/review"
	"github.com/whitel1ght/mrglass/internal/tui"
)

var version = "dev"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		configPath  = flag.String("config", defaultConfigPath(), "path to config.yaml")
		statePath   = flag.String("state", ".mrglass-state.json", "path to snapshot state file")
		noTriage    = flag.Bool("no-triage", false, "disable Claude triage entirely")
		noReview    = flag.Bool("no-review", false, "disable the Claude MR-review hotkey")
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

	// Build the forge provider by config. Both providers satisfy provider.Provider
	// (for the dashboard) and review.ReviewForge (for the review feature).
	var (
		p           provider.Provider
		reviewForge review.ReviewForge
		cliName     string
	)
	switch cfg.Forge {
	case config.ForgeGitHub:
		gp := github.New()
		p, reviewForge, cliName = gp, gp, "gh"
	default: // gitlab
		gp := gitlab.New()
		p, reviewForge, cliName = gp, gp, "glab"
	}

	me, err := p.Whoami()
	if err != nil || me == "" {
		fmt.Fprintf(os.Stderr,
			"Could not determine your %s user — is `%s` installed and authenticated?\n"+
				"Run `%s auth status`, then `%s auth login` if needed.\n",
			cfg.Forge, cliName, cliName, cliName)
		os.Exit(1)
	}

	var az analyze.Analyzer
	if !*noTriage && analyze.Available() {
		cc := analyze.NewClaudeCode()
		az = cc
	}

	m := tui.New(cfg, p, me, az, *statePath)
	// Wire the on-demand review feature ('c'): read-only Claude review of the MR
	// diff, posted only after the user confirms. Needs claude on PATH. Works on
	// whichever forge is configured (both implement review.ReviewForge).
	if !*noReview && review.Available() {
		m = m.WithReview(review.NewClaudeReviewer(), reviewForge)
	}
	// Wire inline ticket status (shown when an MR is expanded). Jira-only for now:
	// needs tickets.status: jira + tickets.jiraBaseURL in config and JIRA_EMAIL/
	// JIRA_API_TOKEN in the env. Absent → feature off; open-in-browser (J) still
	// works via tickets.urlTemplate.
	if cfg.Tickets.Status == "jira" {
		jiraEmail, jiraToken := jira.FromEnv()
		switch {
		case jira.Configured(cfg.Tickets.JiraBaseURL, jiraEmail, jiraToken):
			m = m.WithJira(jira.HTTPClient{BaseURL: cfg.Tickets.JiraBaseURL, Email: jiraEmail, Token: jiraToken})
		case cfg.Tickets.JiraBaseURL == "":
			m = m.WithJiraDisabled("status off: set tickets.jiraBaseURL")
		case jiraEmail == "" || jiraToken == "":
			m = m.WithJiraDisabled("status off: set JIRA_EMAIL + JIRA_API_TOKEN")
		}
	}
	prog := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// defaultConfigPath finds the config, preferring the XDG/~/.config location
// (the conventional spot for a CLI tool, and cross-platform) over Go's
// os.UserConfigDir() — which on macOS is ~/Library/Application Support, an
// unexpected place for a CLI config. Returns the first candidate that exists;
// if none do, returns the ~/.config path as the create-here default.
func defaultConfigPath() string {
	var candidates []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "mrglass", "config.yaml"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "mrglass", "config.yaml"))
	}
	if dir, err := os.UserConfigDir(); err == nil {
		candidates = append(candidates, filepath.Join(dir, "mrglass", "config.yaml"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	if len(candidates) > 0 {
		return candidates[0] // none exist yet → the preferred create-here path
	}
	return "config.yaml"
}
