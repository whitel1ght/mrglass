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
}

func Default() Config {
	return Config{
		Days:           30,
		TicketRegex:    `([A-Z][A-Z0-9]+-\d+)`,
		RefreshMinutes: 5,
		AutoTriage:     true,
		Theme:          "tokyonight",
		Sections: []SectionConfig{
			{Title: "Needs My Review", Filter: `role == "review_requested" && !draft`},
			{Title: "Mine", Filter: `role == "mine"`},
			{Title: "Approved & Green", Filter: `len(approvedBy) > 0 && ci == "success"`},
		},
		Statusline: StatuslineConfig{
			States: map[string]StyleConfig{
				"selected":  {BG: "#2a2a40", Bold: true},
				"ci_failed": {FG: "#e06c75"},
			},
			Left: []Segment{
				{Type: "marker", Source: "role"},
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
