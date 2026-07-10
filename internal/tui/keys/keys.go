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
	OpenTicket  key.Binding
	OpenWork    key.Binding
	Refresh     key.Binding
	Triage      key.Binding
	Review      key.Binding
	Hide        key.Binding
	NextProject key.Binding
	PrevProject key.Binding
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
		Open:        key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open MR in browser")),
		OpenTicket:  key.NewBinding(key.WithKeys("J"), key.WithHelp("J", "open Jira ticket")),
		OpenWork:    key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "work on it (worktree + terminal)")),
		Refresh:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Triage:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "triage")),
		Review:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "claude review")),
		Hide:        key.NewBinding(key.WithKeys("backspace"), key.WithHelp("⌫", "hide/unhide MR")),
		NextProject: key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next project")),
		PrevProject: key.NewBinding(key.WithKeys("["), key.WithHelp("[", "prev project")),
		ToggleAuto:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "toggle auto-triage")),
		Help:        key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp is the always-on bottom bar: a compact subset with terse labels so
// it never overflows the terminal. The full descriptive list lives in FullHelp
// (the '?' overlay). shortLabel overrides only the help text, not the keys.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		shortLabel(k.Down, "j/k", "move"),
		shortLabel(k.Expand, "enter", "open"),
		shortLabel(k.Review, "c", "review"),
		shortLabel(k.Hide, "⌫", "hide"),
		shortLabel(k.Help, "?", "more"),
	}
}

// shortLabel clones a binding with a terser help key/description for the
// bottom bar, leaving the original (and its descriptive FullHelp text) intact.
func shortLabel(b key.Binding, keyText, desc string) key.Binding {
	b.SetHelp(keyText, desc)
	return b
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.NextSection, k.PrevSection, k.Expand, k.Open, k.OpenTicket, k.OpenWork},
		{k.Refresh, k.Triage, k.Review, k.Hide, k.ToggleAuto, k.Help, k.Quit},
	}
}
