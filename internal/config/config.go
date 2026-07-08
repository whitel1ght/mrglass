package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type StyleConfig struct {
	FG    string `yaml:"fg"`
	BG    string `yaml:"bg"`
	Bold  bool   `yaml:"bold"`
	Faint bool   `yaml:"faint"`
}

type Segment struct {
	Type     string                 `yaml:"type"`
	Source   string                 `yaml:"source"`
	When     string                 `yaml:"when"`
	Format   string                 `yaml:"format"`
	Text     string                 `yaml:"text"`
	Align    string                 `yaml:"align"`
	Style    string                 `yaml:"style"`
	Grow     bool                   `yaml:"grow"`
	MaxWidth int                    `yaml:"maxWidth"`
	Symbols  map[string]string      `yaml:"symbols"`
	Styles   map[string]StyleConfig `yaml:"styles"`
}

type StatuslineConfig struct {
	States map[string]StyleConfig `yaml:"states"`
	Left   []Segment              `yaml:"left"`
	Right  []Segment              `yaml:"right"`
}

type SectionConfig struct {
	Title  string `yaml:"title"`
	Filter string `yaml:"filter"`
}

type Config struct {
	Days           int              `yaml:"days"`
	TicketRegex    string           `yaml:"ticketRegex"`
	RefreshMinutes int              `yaml:"refreshMinutes"`
	AutoTriage     bool             `yaml:"autoTriage"`
	Theme          string           `yaml:"theme"`
	Sections       []SectionConfig  `yaml:"sections"`
	Statusline     StatuslineConfig `yaml:"statusline"`
	// ReviewPrompt is the instruction given to Claude for the on-demand MR
	// review (the 'c' hotkey). The MR diff is appended to it.
	ReviewPrompt string `yaml:"reviewPrompt"`
	// ReviewSkill, when set, makes the review invoke a Claude skill (e.g.
	// "superpowers:requesting-code-review" or "claude-components:mr-review") via
	// the Skill tool. mrglass verifies the skill actually ran and reports it.
	// Empty -> plain prompt review.
	ReviewSkill string `yaml:"reviewSkill"`
	// PluginDirs are extra directories loaded into the review's claude run via
	// --plugin-dir (session-scoped; does NOT touch your global ~/.claude). Point
	// at a checkout like ~/projects/claude-components to use its skills; the
	// skill is then namespaced by the dir name, e.g. "claude-components:mr-review".
	PluginDirs []string `yaml:"pluginDirs"`
	// ProjectsDir is the base directory holding your local clones. When set, the
	// review runs with full project context: mrglass finds the matching clone,
	// checks out the MR branch in a throwaway git worktree (your working copy is
	// never touched), and runs Claude there so it sees the repo's CLAUDE.md,
	// .claude/skills, and all files. Empty -> diff-only review.
	ProjectsDir string `yaml:"projectsDir"`
	// ProjectPaths overrides the clone location per MR project path
	// (e.g. "group/repo": "/abs/path"). Takes precedence over ProjectsDir.
	ProjectPaths map[string]string `yaml:"projectPaths"`
	// Forge selects the code host: "gitlab" (via glab) or "github" (via gh).
	Forge string `yaml:"forge"`
	// Tickets configures the issue tracker integration.
	Tickets TicketsConfig `yaml:"tickets"`
	// Worktree configures the `w` hotkey: open the MR branch in a terminal.
	Worktree WorktreeConfig `yaml:"worktree"`

	// Jira is the legacy Jira config; superseded by Tickets. Kept so existing
	// configs migrate (see migrateLegacyJira). Do not use in new configs.
	Jira JiraConfig `yaml:"jira"`
}

// WorktreeConfig configures the `w` hotkey, which checks out the MR branch in a
// dedicated git worktree and opens it in a terminal running WorkCmd.
type WorktreeConfig struct {
	// OpenCommand is run (detached) to open the worktree. Placeholders mrglass
	// fills: {dir} (worktree path), {cmd} (WorkCmd), {branch}, {key}. Empty
	// disables `w`. E.g. "tmux new-window -c {dir} {cmd}".
	OpenCommand string `yaml:"openCommand"`
	// WorkCmd is what to run in the worktree (substituted as {cmd}); default "claude".
	WorkCmd string `yaml:"workCmd"`
	// Dir overrides where worktrees are created (default: <clone>/../.mrglass-worktrees).
	Dir string `yaml:"dir"`
}

// TicketsConfig configures the issue tracker. Open-in-browser works for ANY
// tracker via URLTemplate; inline status is Jira-only for now.
type TicketsConfig struct {
	// URLTemplate builds the ticket URL; "{key}" is replaced with the ticket key.
	// Tracker-agnostic, e.g. "https://acme.atlassian.net/browse/{key}" or
	// "https://linear.app/acme/issue/{key}". Empty disables open-in-browser.
	URLTemplate string `yaml:"urlTemplate"`
	// Status selects the inline-status backend: "jira" or "none" (default).
	Status string `yaml:"status"`
	// JiraBaseURL is the Jira site root used for inline status when Status=="jira"
	// (needs JIRA_EMAIL/JIRA_API_TOKEN in the env).
	JiraBaseURL string `yaml:"jiraBaseURL"`
}

// JiraConfig is the legacy Jira section (pre-Tickets). Retained for migration.
type JiraConfig struct {
	BaseURL string `yaml:"baseURL"`
}

// ForgeGitLab / ForgeGitHub are the supported forge values.
const (
	ForgeGitLab = "gitlab"
	ForgeGitHub = "github"
)

// DefaultReviewPrompt is the built-in MR-review instruction.
const DefaultReviewPrompt = "You are reviewing a GitLab merge request. " +
	"Given the diff below, write concise, actionable review feedback as a Markdown " +
	"comment. Group findings by severity (Critical / Important / Minor); call out " +
	"bugs, correctness issues, and risky changes first. If it looks good, say so " +
	"briefly. No preamble — output only the review comment."

func Default() Config {
	return Config{
		Days:           30,
		TicketRegex:    `([A-Z][A-Z0-9]+-\d+)`,
		RefreshMinutes: 5,
		AutoTriage:     true,
		Theme:          "tokyonight",
		Forge:          ForgeGitLab,
		ReviewPrompt:   DefaultReviewPrompt,
		Worktree:       WorktreeConfig{WorkCmd: "claude"},
		Sections: []SectionConfig{
			{Title: "Needs My Review", Filter: `role == "review_requested" && !draft`},
			{Title: "Mine · Approved", Filter: `role == "mine" && len(approvedBy) > 0`},
			{Title: "Mine · Needs approval", Filter: `role == "mine" && len(approvedBy) == 0`},
		},
		Statusline: StatuslineConfig{
			States: map[string]StyleConfig{
				"selected":  {BG: "#2a2a40", Bold: true},
				"ci_failed": {FG: "#e06c75"},
			},
			Left: []Segment{
				// Role is conveyed by the active section/tab, so no per-row role
				// marker by default. (Add {type: marker} to bring it back.)
				{Type: "text", Source: "title", Grow: true, MaxWidth: 60},
			},
			Right: []Segment{
				{Type: "ci", When: `ci != ""`,
					Symbols: map[string]string{"success": "✓", "failed": "✗", "running": "🔄"}},
				{Type: "approvals"},
				{Type: "comments", When: "comments > 0", Format: "💬{comments}"},
				{Type: "advice", When: "hasAdvice", Text: "💡"},
				{Type: "age", Source: "updatedAt", Align: "right", Style: "faint"},
			},
		},
	}
}

// Load starts from Default() and overlays the YAML file at path. It never errors:
// a missing or broken file degrades to defaults with a warning.
func Load(path string) (Config, []string) {
	cfg := Default()
	var warns []string
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			warns = append(warns, fmt.Sprintf("config %s not found; using defaults", path))
		} else {
			warns = append(warns, fmt.Sprintf("could not read %s: %v; using defaults", path, err))
		}
		return cfg, warns
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		warns = append(warns, fmt.Sprintf("config %s is invalid: %v; using defaults", path, err))
		return Default(), warns
	}
	warns = append(warns, cfg.normalize()...)
	return cfg, warns
}

// normalize applies defaults/migrations after unmarshal. Returns any warnings.
func (c *Config) normalize() []string {
	var warns []string

	// Forge: default + validate.
	switch c.Forge {
	case "":
		c.Forge = ForgeGitLab
	case ForgeGitLab, ForgeGitHub:
		// ok
	default:
		warns = append(warns, fmt.Sprintf("unknown forge %q; falling back to %q", c.Forge, ForgeGitLab))
		c.Forge = ForgeGitLab
	}

	// ticketRegex: must compile and have a capture group (ParseTicket uses m[1]).
	if re, err := regexp.Compile(c.TicketRegex); err != nil {
		warns = append(warns, fmt.Sprintf("invalid ticketRegex %q: %v; using default", c.TicketRegex, err))
		c.TicketRegex = Default().TicketRegex
	} else if re.NumSubexp() < 1 {
		warns = append(warns, fmt.Sprintf("ticketRegex %q has no capture group; using default", c.TicketRegex))
		c.TicketRegex = Default().TicketRegex
	}

	// Migrate legacy jira.baseURL → tickets.* when tickets isn't configured.
	if c.Jira.BaseURL != "" && c.Tickets.URLTemplate == "" {
		base := strings.TrimRight(c.Jira.BaseURL, "/")
		c.Tickets.URLTemplate = base + "/browse/{key}"
		if c.Tickets.JiraBaseURL == "" {
			c.Tickets.JiraBaseURL = base
		}
		if c.Tickets.Status == "" {
			c.Tickets.Status = "jira"
		}
		warns = append(warns, "config: jira.baseURL is deprecated — migrated to tickets.urlTemplate; "+
			"please update your config")
	}
	c.Jira = JiraConfig{} // clear legacy so nothing else reads it
	return warns
}
