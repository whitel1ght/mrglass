package keys

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Up          key.Binding
	Down        key.Binding
	Top         key.Binding
	Bottom      key.Binding
	NextSection key.Binding
	PrevSection key.Binding
	Expand      key.Binding
	Open        key.Binding
	Refresh     key.Binding
	Triage      key.Binding
	Review      key.Binding
	ToggleAuto  key.Binding
	Help        key.Binding
	Quit        key.Binding
}

func Default() KeyMap {
	return KeyMap{
		Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Top:         key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "top")),
		Bottom:      key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom")),
		NextSection: key.NewBinding(key.WithKeys("tab", "l"), key.WithHelp("tab/l", "next section")),
		PrevSection: key.NewBinding(key.WithKeys("shift+tab", "h"), key.WithHelp("⇧tab/h", "prev section")),
		Expand:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "expand/collapse")),
		Open:        key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open in browser")),
		Refresh:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Triage:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "triage")),
		Review:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "claude review")),
		ToggleAuto:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "toggle auto-triage")),
		Help:        key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.NextSection, k.Expand, k.Open, k.Review, k.Refresh, k.Help, k.Quit}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.NextSection, k.PrevSection, k.Expand, k.Open},
		{k.Refresh, k.Triage, k.Review, k.ToggleAuto, k.Help, k.Quit},
	}
}
