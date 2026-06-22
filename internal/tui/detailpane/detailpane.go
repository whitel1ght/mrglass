package detailpane

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/dmitry/mrglass/internal/core"
	"github.com/dmitry/mrglass/internal/jira"
	"github.com/dmitry/mrglass/internal/tui/theme"
)

const indent = "      " // detail lines sit under the row's title

// TicketView carries the Jira-ticket state for the expanded detail. Show is
// false when the MR has no ticket key or Jira isn't configured (render nothing).
type TicketView struct {
	Show    bool
	Key     string
	Loading bool
	Err     bool
	T       jira.Ticket
}

// Render builds the inline detail block shown beneath an expanded MR row:
// indented, dimmed metadata, the Jira ticket line (if any), and Claude advice.
// Returns the lines joined by "\n" (no trailing newline).
func Render(st theme.Styles, mr core.MR, advice string, tv TicketView) string {
	var lines []string
	add := func(s string) { lines = append(lines, indent+s) }

	add(st.Subtle.Render(mr.Ref))
	add(st.Subtle.Render(fmt.Sprintf("%s → %s", mr.SourceBranch, mr.TargetBranch)))
	add(st.Base.Render("CI: ") + ciText(st, mr.CI))
	if len(mr.ApprovedBy) > 0 {
		add(st.Success.Render("approved by " + strings.Join(mr.ApprovedBy, ", ")))
	} else {
		add(st.Subtle.Render("awaiting approval"))
	}
	if mr.Conflicts {
		add(st.Danger.Render("⚠ conflicts"))
	}
	if mr.Unresolved {
		add(st.Warn.Render("unresolved threads"))
	}
	for _, l := range ticketLines(st, tv) {
		add(l)
	}
	if advice != "" {
		add(st.Advice.Render("💡 " + advice))
	}
	return strings.Join(lines, "\n")
}

// ticketLines renders the 🎫 Jira line(s): loading / error / status+assignee +
// summary. Empty slice when there's no ticket to show.
func ticketLines(st theme.Styles, tv TicketView) []string {
	if !tv.Show || tv.Key == "" {
		return nil
	}
	switch {
	case tv.Loading:
		return []string{st.Subtle.Render("🎫 " + tv.Key + " · loading…")}
	case tv.Err:
		return []string{st.Subtle.Render("🎫 " + tv.Key + " · (status unavailable)")}
	default:
		status := statusStyle(st, tv.T.StatusCategory).Render(tv.T.Status)
		head := st.Base.Render("🎫 "+tv.Key+" · ") + status +
			st.Subtle.Render(" · "+tv.T.Assignee)
		out := []string{head}
		if tv.T.Summary != "" {
			out = append(out, st.Subtle.Render("   "+tv.T.Summary))
		}
		return out
	}
}

// statusStyle colors a Jira status by its category.
func statusStyle(st theme.Styles, category string) lipgloss.Style {
	switch category {
	case "done":
		return st.Success
	case "indeterminate": // In Progress
		return st.Accent
	default: // "new" (To Do) or unknown
		return st.Subtle
	}
}

func ciText(st theme.Styles, status string) string {
	switch status {
	case "success":
		return st.Success.Render("passed")
	case "failed", "canceled":
		return st.Danger.Render(dash(status))
	case "running", "pending", "created", "manual":
		return st.Warn.Render(dash(status))
	default:
		return st.Subtle.Render(dash(status))
	}
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
