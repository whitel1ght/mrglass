package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// logoGlass is the magnifying glass; logoWord is the shadow-font "mrglass".
// They're stored separately so each can carry its own style, then joined
// side by side. Kept as raw string blocks (one visual unit) — do not reflow.
const logoGlass = `` +
	"    ___    \n" +
	"  ,'   `.  \n" +
	" / ,---. \\ \n" +
	"| | · · | |\n" +
	" \\ `---' / \n" +
	"  `._,_.'\\ \n" +
	"         \\ \n" +
	"          \\"

const logoWord = `` +
	` __ ` + "`" + `__ \   __| _` + "`" + ` | |  _` + "`" + ` |  __|  __|` + "\n" +
	` |   |   | |   (   | | (   |\__ \\__ \` + "\n" +
	`_|  _|  _|_|  \__, |_|\__,_|____/____/` + "\n" +
	`              |___/`

const logoTagline = "your open merge requests, at a glance"

// splashView is the centered startup logo shown until the first fetch returns.
// The whole logo is placed as one block so the glass and wordmark stay aligned;
// on a terminal too small for the art it degrades to a compact one-line splash.
func (m Model) splashView() string {
	spin := m.spinner.View()
	loading := m.styles.Subtle.Render(spin + " loading your merge requests…")

	// Compact fallback: the art needs ~52 cols and ~11 rows. Below that, a
	// single centered "mrglass · loading…" avoids a wrapped, broken logo.
	logoW := lipgloss.Width(logoGlass) + 2 + lipgloss.Width(logoWord)
	if m.width < logoW+2 || m.height < 12 {
		compact := m.styles.Accent.Render("mrglass") + m.styles.Subtle.Render(" · loading…")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, compact)
	}

	glass := m.styles.Accent.Render(logoGlass)
	word := m.styles.Accent.Render(logoWord)
	lockup := lipgloss.JoinHorizontal(lipgloss.Center, glass, "  ", word)

	block := lipgloss.JoinVertical(
		lipgloss.Center,
		lockup,
		"",
		m.styles.Subtle.Render(logoTagline),
		"",
		loading,
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, block)
}

// splashActive reports whether the startup splash should render: the first
// fetch hasn't returned and we're not in a takeover state (help/review).
func (m Model) splashActive() bool {
	return !m.loaded && !m.showHelp && m.pendingReview == nil && !strings.HasPrefix(m.status, "⚠")
}
