package config

import (
	"fmt"
	"os"

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
	// "superpowers:requesting-code-review") via the Skill tool. mrglass verifies
	// the skill actually ran and reports it. Empty -> plain prompt review.
	ReviewSkill string `yaml:"reviewSkill"`
	// ProjectsDir is the base directory holding your local clones. When set, the
	// review runs with full project context: mrglass finds the matching clone,
	// checks out the MR branch in a throwaway git worktree (your working copy is
	// never touched), and runs Claude there so it sees the repo's CLAUDE.md,
	// .claude/skills, and all files. Empty -> diff-only review.
	ProjectsDir string `yaml:"projectsDir"`
	// ProjectPaths overrides the clone location per MR project path
	// (e.g. "group/repo": "/abs/path"). Takes precedence over ProjectsDir.
	ProjectPaths map[string]string `yaml:"projectPaths"`
}

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
		ReviewPrompt:   DefaultReviewPrompt,
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
	return cfg, warns
}
