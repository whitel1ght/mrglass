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
// group is flush-right; a left-group grow segment absorbs slack.
func Render(cfg config.StatuslineConfig, st theme.Styles, rv RowView, width int, selected bool) string {
	env := exprEnv(rv)
	left := renderGroup(cfg.Left, rv, env)
	right := renderGroup(cfg.Right, rv, env)

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	if selected {
		return st.Selected.Render(line)
	}
	return st.Base.Render(line)
}

func renderGroup(segs []config.Segment, rv RowView, env map[string]any) string {
	var parts []string
	for _, s := range segs {
		if s.When != "" && !evalBool(s.When, env) {
			continue
		}
		if txt := renderSegment(s, rv); txt != "" {
			parts = append(parts, txt)
		}
	}
	return strings.Join(parts, " ")
}

func renderSegment(s config.Segment, rv RowView) string {
	mr := rv.MR
	switch s.Type {
	case "marker":
		if mr.Role == core.RoleMine {
			return "▸"
		}
		return "◇"
	case "text":
		v := fieldString(s.Source, mr)
		if s.MaxWidth > 0 && len(v) > s.MaxWidth {
			v = v[:s.MaxWidth-1] + "…"
		}
		return v
	case "ci":
		if sym, ok := s.Symbols[mr.CI]; ok {
			return sym
		}
		return mr.CI
	case "approvals":
		f := s.Format
		f = strings.ReplaceAll(f, "{approved}", fmt.Sprint(len(mr.ApprovedBy)))
		f = strings.ReplaceAll(f, "{required}", fmt.Sprint(rv.ApprovalsRequired))
		return f
	case "comments":
		return strings.ReplaceAll(s.Format, "{comments}", fmt.Sprint(mr.Comments))
	case "advice":
		if rv.HasAdvice {
			return s.Text
		}
		return ""
	case "age":
		return relAge(mr.UpdatedAt)
	default:
		return "" // unknown type: skip, never panic
	}
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
