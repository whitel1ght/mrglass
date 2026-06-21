package detailpane

import (
	"fmt"
	"strings"

	"github.com/dmitry/mrglass/internal/core"
	"github.com/dmitry/mrglass/internal/tui/theme"
)

// Render builds the right-hand detail view for one MR plus any Claude advice.
func Render(st theme.Styles, mr core.MR, advice string, width int) string {
	var b strings.Builder
	b.WriteString(st.Header.Render(mr.Ref) + "\n")
	b.WriteString(st.Base.Render(mr.Title) + "\n\n")
	b.WriteString(st.Base.Render(fmt.Sprintf("%s → %s", mr.SourceBranch, mr.TargetBranch)) + "\n")
	b.WriteString(st.Base.Render("CI: "+dash(mr.CI)) + "\n")
	if len(mr.ApprovedBy) > 0 {
		b.WriteString(st.Base.Render("approved by "+strings.Join(mr.ApprovedBy, ", ")) + "\n")
	}
	if mr.Conflicts {
		b.WriteString(st.Base.Render("⚠ conflicts") + "\n")
	}
	if advice != "" {
		b.WriteString("\n" + st.Advice.Render("💡 "+advice) + "\n")
	}
	return b.String()
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
