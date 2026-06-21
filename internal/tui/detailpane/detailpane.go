package detailpane

import (
	"fmt"
	"strings"

	"github.com/dmitry/mrglass/internal/core"
	"github.com/dmitry/mrglass/internal/tui/theme"
)

const indent = "      " // detail lines sit under the row's title

// Render builds the inline detail block shown beneath an expanded MR row:
// indented, dimmed metadata plus any Claude advice. Returns the lines joined
// by "\n" (no trailing newline).
func Render(st theme.Styles, mr core.MR, advice string) string {
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
	if advice != "" {
		add(st.Advice.Render("💡 " + advice))
	}
	return strings.Join(lines, "\n")
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
