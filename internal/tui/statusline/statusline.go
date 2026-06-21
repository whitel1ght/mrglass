package statusline

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/expr-lang/expr"
	"github.com/dmitry/mrglass/internal/config"
	"github.com/dmitry/mrglass/internal/core"
	"github.com/dmitry/mrglass/internal/tui/theme"
)

// RowView is the render input: an MR plus UI-only facts.
type RowView struct {
	MR                core.MR
	HasAdvice         bool
	ApprovalsRequired int
}

// Render produces one MR row sized to width. Left group is left-aligned; right
// group is flush-right. Each segment is colored by meaning from the theme; the
// selected row gets a background bar that the segment foregrounds render over.
func Render(cfg config.StatuslineConfig, st theme.Styles, rv RowView, width int, selected bool) string {
	env := exprEnv(rv)
	left := renderGroup(cfg.Left, st, rv, env)
	right := renderGroup(cfg.Right, st, rv, env)

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	if selected {
		// Wrap the already-colored line in the selection background. The
		// per-segment foregrounds are baked in; this adds the bar behind them.
		return st.Selected.Render(line)
	}
	return line
}

func renderGroup(segs []config.Segment, st theme.Styles, rv RowView, env map[string]any) string {
	var parts []string
	for _, s := range segs {
		if s.When != "" && !evalBool(s.When, env) {
			continue
		}
		if txt := renderSegment(s, st, rv); txt != "" {
			parts = append(parts, txt)
		}
	}
	return strings.Join(parts, " ")
}

// renderSegment produces the (already-styled) text for one segment. A built-in
// semantic style is chosen from the theme by segment type/value; an explicit
// per-segment config Style/Styles override is applied on top when present.
func renderSegment(s config.Segment, st theme.Styles, rv RowView) string {
	mr := rv.MR
	var text string
	style := st.Base // default foreground

	switch s.Type {
	case "marker":
		// Role indicator. Deliberately NOT a triangle — the list prefixes each
		// row with a ▸/▾ disclosure marker, so a dot/ring keeps the two distinct.
		if mr.Role == core.RoleMine {
			text, style = "●", st.Accent
		} else {
			text, style = "○", st.Subtle
		}
	case "text":
		text = fieldString(s.Source, mr)
		if s.MaxWidth > 0 {
			if r := []rune(text); len(r) > s.MaxWidth {
				text = string(r[:s.MaxWidth-1]) + "…"
			}
		}
		style = st.Base
	case "ci":
		if sym, ok := s.Symbols[mr.CI]; ok {
			text = sym
		} else {
			text = mr.CI
		}
		style = ciStyle(st, mr.CI)
	case "approvals":
		// Glanceable yes/no: green ✓ when approved, dim ○ otherwise — always in
		// the same column so it scans down the list. (The approver count and
		// names live in the expanded detail.) A custom Format still wins.
		if core.Approved(mr.ApprovedBy, rv.ApprovalsRequired) {
			text, style = "✓", st.Success
		} else {
			text, style = "○", st.Subtle
		}
		if s.Format != "" {
			text = s.Format
			text = strings.ReplaceAll(text, "{approved}", fmt.Sprint(len(mr.ApprovedBy)))
			text = strings.ReplaceAll(text, "{required}", fmt.Sprint(rv.ApprovalsRequired))
		}
	case "comments":
		text = strings.ReplaceAll(s.Format, "{comments}", fmt.Sprint(mr.Comments))
		style = st.Base
	case "advice":
		if !rv.HasAdvice {
			return ""
		}
		text, style = s.Text, st.Advice
	case "age":
		text, style = relAge(mr.UpdatedAt), st.Subtle
	default:
		return "" // unknown type: skip, never panic
	}

	if text == "" {
		return ""
	}
	// Explicit per-segment config style wins over the semantic default.
	if override, ok := configStyle(s, mr); ok {
		style = override
	}
	return style.Inline(true).Render(text)
}

// ciStyle maps a CI status to a semantic theme style.
func ciStyle(st theme.Styles, status string) lipgloss.Style {
	switch status {
	case "success":
		return st.Success
	case "failed", "canceled":
		return st.Danger
	case "running", "pending", "created", "manual":
		return st.Warn
	default:
		return st.Subtle
	}
}

// configStyle returns an explicit per-segment style from config, if any. A
// segment may carry a single Style name or a per-value Styles map (keyed, for
// ci, by status). Returns ok=false when no override applies.
func configStyle(s config.Segment, mr core.MR) (lipgloss.Style, bool) {
	if s.Type == "ci" && len(s.Styles) > 0 {
		if sc, ok := s.Styles[mr.CI]; ok {
			return theme.StyleFrom(sc), true
		}
	}
	return lipgloss.Style{}, false
}

func fieldString(source string, mr core.MR) string {
	switch source {
	case "title":
		return mr.Title
	case "ref":
		return mr.Ref
	case "role":
		return mr.Role.String()
	default:
		return ""
	}
}

func relAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// exprEnv exposes MR fields to `when` predicates with the same names sections use.
func exprEnv(rv RowView) map[string]any {
	mr := rv.MR
	return map[string]any{
		"role":       mr.Role.String(),
		"ci":         mr.CI,
		"draft":      mr.Draft,
		"conflicts":  mr.Conflicts,
		"unresolved": mr.Unresolved,
		"comments":   mr.Comments,
		"approvedBy": mr.ApprovedBy,
		"required":   rv.ApprovalsRequired,
		"hasAdvice":  rv.HasAdvice,
	}
}

func evalBool(code string, env map[string]any) bool {
	prog, err := expr.Compile(code, expr.Env(env), expr.AsBool())
	if err != nil {
		return false
	}
	out, err := expr.Run(prog, env)
	if err != nil {
		return false
	}
	b, _ := out.(bool)
	return b
}
