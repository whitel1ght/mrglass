package theme

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/dmitry/mrglass/internal/config"
)

type Theme struct {
	Name       string
	Text       lipgloss.Color
	Subtle     lipgloss.Color
	Success    lipgloss.Color
	Warn       lipgloss.Color
	Danger     lipgloss.Color
	SelectedBG lipgloss.Color
}

type Styles struct {
	Base     lipgloss.Style
	Selected lipgloss.Style
	Header   lipgloss.Style
	Footer   lipgloss.Style
	Help     lipgloss.Style
	Advice   lipgloss.Style
}

func Registry() map[string]Theme {
	return map[string]Theme{
		"default": {
			Name: "default", Text: "#e2e1ed", Subtle: "#6c6f85",
			Success: "#3df294", Warn: "#e5c07b", Danger: "#e06c75",
			SelectedBG: "#2a2a40",
		},
		"dracula": {
			Name: "dracula", Text: "#f8f8f2", Subtle: "#6272a4",
			Success: "#50fa7b", Warn: "#f1fa8c", Danger: "#ff5555",
			SelectedBG: "#44475a",
		},
	}
}

func Get(name string) Theme {
	if t, ok := Registry()[name]; ok {
		return t
	}
	return Registry()["default"]
}

func BuildStyles(t Theme) Styles {
	return Styles{
		Base:     lipgloss.NewStyle().Foreground(t.Text),
		Selected: lipgloss.NewStyle().Foreground(t.Text).Background(t.SelectedBG).Bold(true),
		Header:   lipgloss.NewStyle().Foreground(t.Text).Bold(true),
		Footer:   lipgloss.NewStyle().Foreground(t.Subtle),
		Help:     lipgloss.NewStyle().Foreground(t.Subtle),
		Advice:   lipgloss.NewStyle().Foreground(t.Success),
	}
}

func StyleFrom(sc config.StyleConfig) lipgloss.Style {
	s := lipgloss.NewStyle()
	if sc.FG != "" {
		s = s.Foreground(lipgloss.Color(sc.FG))
	}
	if sc.BG != "" {
		s = s.Background(lipgloss.Color(sc.BG))
	}
	if sc.Bold {
		s = s.Bold(true)
	}
	if sc.Faint {
		s = s.Faint(true)
	}
	return s
}
