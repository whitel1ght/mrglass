package statusline

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/whitel1ght/mrglass/internal/config"
	"github.com/whitel1ght/mrglass/internal/core"
	"github.com/whitel1ght/mrglass/internal/tui/theme"
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

	// Row-state overrides from config. "ci_failed" replaces the BASE style
	// (plain text like the title) so the whole row reads as failed; semantic
	// segment colors (the ci symbol itself, approvals) are unaffected.
	base := st.Base
	if rv.MR.CI == "failed" {
		if sc, ok := cfg.States["ci_failed"]; ok {
			base = theme.StyleFrom(sc)
		}
	}

	left := renderGroup(cfg.Left, st, rv, env, base)
	right := renderGroup(cfg.Right, st, rv, env, base)

	// A grow segment absorbs width pressure: when the row would overflow the
	// terminal, re-render its group with the grow segment shrunk by the
	// overflow (floor 4 runes) so the row fits instead of wrapping.
	if over := lipgloss.Width(left) + 1 + lipgloss.Width(right) - width; over > 0 {
		if segs, i := findGrow(cfg.Left, env); i >= 0 {
			left = renderShrunk(segs, i, over, st, rv, env, base)
		} else if segs, i := findGrow(cfg.Right, env); i >= 0 {
			right = renderShrunk(segs, i, over, st, rv, env, base)
		}
	}

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	if selected {
		// Wrap the already-colored line in the selection background. The
		// per-segment foregrounds are baked in; this adds the bar behind them.
		// "selected" replaces the theme's selection bar when configured.
		sel := st.Selected
		if sc, ok := cfg.States["selected"]; ok {
			sel = theme.StyleFrom(sc)
		}
		return sel.Render(line)
	}
	return line
}

func renderGroup(segs []config.Segment, st theme.Styles, rv RowView, env map[string]any, base lipgloss.Style) string {
	var parts []string
	for _, s := range segs {
		if s.When != "" && !evalBool(s.When, env) {
			continue
		}
		if txt := renderSegment(s, st, rv, base); txt != "" {
			parts = append(parts, txt)
		}
	}
	return strings.Join(parts, " ")
}

// findGrow returns the segments and the index of the first Grow segment that
// is actually visible on this row, or -1. A segment hidden by its When
// predicate is skipped: renderGroup would drop it regardless, so shrinking it
// would be a no-op that leaves the row silently overflowing. Visibility uses
// the same evalBool check renderGroup applies.
func findGrow(segs []config.Segment, env map[string]any) ([]config.Segment, int) {
	for i, s := range segs {
		if s.When != "" && !evalBool(s.When, env) {
			continue
		}
		if s.Grow {
			return segs, i
		}
	}
	return nil, -1
}

// renderShrunk re-renders a group with segment i's MaxWidth reduced by over.
func renderShrunk(segs []config.Segment, i, over int, st theme.Styles, rv RowView, env map[string]any, base lipgloss.Style) string {
	s := segs[i]
	cur := lipgloss.Width(renderSegment(s, st, rv, base))
	newMax := cur - over
	if newMax < 4 {
		newMax = 4
	}
	s.MaxWidth = newMax
	out := make([]config.Segment, len(segs))
	copy(out, segs)
	out[i] = s
	return renderGroup(out, st, rv, env, base)
}

// truncate cuts s to max runes, ending with … when cut. max <= 0 means no limit.
func truncate(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

// renderSegment produces the (already-styled) text for one segment. A built-in
// semantic style is chosen from the theme by segment type/value; an explicit
// per-segment config Style/Styles override is applied on top when present.
func renderSegment(s config.Segment, st theme.Styles, rv RowView, base lipgloss.Style) string {
	mr := rv.MR
	var text string
	style := base // default foreground

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
		text = truncate(fieldString(s.Source, mr), s.MaxWidth)
		style = base
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
		style = base
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
	if override, ok := configStyle(s, st, mr); ok {
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

// configStyle returns an explicit per-segment style from config, if any.
// Precedence: per-value Styles map (ci, keyed by status) → named Style →
// none. Unknown style names are ignored (semantic default kept) — config
// mistakes must never break rendering.
func configStyle(s config.Segment, st theme.Styles, mr core.MR) (lipgloss.Style, bool) {
	if s.Type == "ci" && len(s.Styles) > 0 {
		if sc, ok := s.Styles[mr.CI]; ok {
			return theme.StyleFrom(sc), true
		}
	}
	if s.Style != "" {
		return namedStyle(st, s.Style)
	}
	return lipgloss.Style{}, false
}

// namedStyle resolves a config style name to a theme style.
func namedStyle(st theme.Styles, name string) (lipgloss.Style, bool) {
	switch name {
	case "base":
		return st.Base, true
	case "subtle":
		return st.Subtle, true
	case "faint":
		return st.Subtle.Faint(true), true
	case "accent":
		return st.Accent, true
	case "success":
		return st.Success, true
	case "warn":
		return st.Warn, true
	case "danger":
		return st.Danger, true
	case "advice":
		return st.Advice, true
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

// whenProgs caches compiled `when` predicates (fixed config strings).
var whenProgs sync.Map // code string -> *vm.Program

func evalBool(code string, env map[string]any) bool {
	var prog *vm.Program
	if v, ok := whenProgs.Load(code); ok {
		prog = v.(*vm.Program)
	} else {
		p, err := expr.Compile(code, expr.Env(env), expr.AsBool())
		if err != nil {
			return false
		}
		whenProgs.Store(code, p)
		prog = p
	}
	out, err := expr.Run(prog, env)
	if err != nil {
		return false
	}
	b, _ := out.(bool)
	return b
}
