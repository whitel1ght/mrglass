package statusline

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/whitel1ght/mrglass/internal/config"
	"github.com/whitel1ght/mrglass/internal/core"
	"github.com/whitel1ght/mrglass/internal/tui/theme"
)

func baseRow() RowView {
	return RowView{
		MR: core.MR{
			Ref: "g/p!1", Title: "feat: thing", Role: core.RoleMine,
			CI: "failed", Comments: 2,
		},
		HasAdvice:         true,
		ApprovalsRequired: 2,
	}
}

func cfg() config.StatuslineConfig {
	return config.Default().Statusline
}

func TestRenderColorsCIByStatus(t *testing.T) {
	// Force a color profile so lipgloss actually emits ANSI escapes; otherwise
	// in a non-TTY test env it degrades to plain text and we can't observe color.
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	st := theme.BuildStyles(theme.Get("tokyonight"))

	failed := baseRow()
	failed.MR.CI = "failed"
	passed := baseRow()
	passed.MR.CI = "success"

	outFailed := Render(cfg(), st, failed, 80, false)
	outPassed := Render(cfg(), st, passed, 80, false)

	// Each row must contain ANSI escape sequences (it is colored, not plain).
	if !strings.Contains(outFailed, "\x1b[") {
		t.Errorf("expected ANSI color escapes in a colored row, got %q", outFailed)
	}
	// The danger color (failed) and success color (passed) differ, so the two
	// rows must not be identical once the CI status differs.
	if outFailed == outPassed {
		t.Error("failed and passed CI rows should render with different colors")
	}
	// The Tokyo Night danger red (#f7768e) should appear for a failed CI.
	if !strings.Contains(outFailed, "247") { // 0xf7 = 247, part of the truecolor SGR
		t.Logf("failed row: %q", outFailed) // informational; SGR encoding may vary
	}
}

func TestRenderIncludesTitleAndCISymbol(t *testing.T) {
	out := Render(cfg(), theme.BuildStyles(theme.Get("default")), baseRow(), 80, false)
	if !strings.Contains(out, "feat: thing") {
		t.Errorf("row should contain the title: %q", out)
	}
	if !strings.Contains(out, "✗") {
		t.Errorf("failed CI should render ✗: %q", out)
	}
}

func TestRenderApprovalMark(t *testing.T) {
	st := theme.BuildStyles(theme.Get("default"))

	// Not approved: 0 of 2 required -> dim ○, no ✓.
	notApproved := baseRow() // baseRow has ApprovalsRequired:2, no approvers
	out := Render(cfg(), st, notApproved, 80, false)
	if !strings.Contains(out, "○") {
		t.Errorf("unapproved MR should show ○: %q", out)
	}

	// Approved: 2 of 2 required -> green ✓.
	approved := baseRow()
	approved.MR.ApprovedBy = []string{"alice", "bob"}
	out = Render(cfg(), st, approved, 80, false)
	if !strings.Contains(out, "✓") {
		t.Errorf("approved MR should show ✓: %q", out)
	}
}

func TestRenderHidesCommentsWhenZero(t *testing.T) {
	rv := baseRow()
	rv.MR.Comments = 0
	out := Render(cfg(), theme.BuildStyles(theme.Get("default")), rv, 80, false)
	if strings.Contains(out, "💬") {
		t.Errorf("zero comments should hide the comments segment: %q", out)
	}
}

func TestRenderShowsAdviceMarkerOnlyWhenAdvice(t *testing.T) {
	st := theme.BuildStyles(theme.Get("default"))
	with := Render(cfg(), st, baseRow(), 80, false)
	if !strings.Contains(with, "💡") {
		t.Errorf("HasAdvice should show 💡: %q", with)
	}
	rv := baseRow()
	rv.HasAdvice = false
	without := Render(cfg(), st, rv, 80, false)
	if strings.Contains(without, "💡") {
		t.Errorf("no advice should hide 💡: %q", without)
	}
}

func TestRenderUnknownSegmentDoesNotPanic(t *testing.T) {
	c := cfg()
	c.Right = append(c.Right, config.Segment{Type: "bogus"})
	_ = Render(c, theme.BuildStyles(theme.Get("default")), baseRow(), 80, false)
}

func TestRenderTruncatesMultibyteTitleSafely(t *testing.T) {
	rv := baseRow()
	rv.MR.Title = "café " + strings.Repeat("ü", 80) // 85 runes, all multibyte
	// Default cfg has maxWidth 60 for the title text segment, so truncation fires.
	out := Render(cfg(), theme.BuildStyles(theme.Get("default")), rv, 200, false)
	if !utf8.ValidString(out) {
		t.Errorf("rendered output is not valid UTF-8: %q", out)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("truncated title should contain ellipsis: %q", out)
	}
}

func TestSegmentStyleNameHonored(t *testing.T) {
	st := theme.BuildStyles(theme.Get("tokyonight"))
	seg := config.Segment{Type: "text", Source: "title", Style: "danger"}
	rv := RowView{MR: core.MR{Title: "hello"}}
	got := renderSegment(seg, st, rv, st.Base)
	want := st.Danger.Inline(true).Render("hello")
	if got != want {
		t.Errorf("style name not honored:\n got %q\nwant %q", got, want)
	}
}

func TestGrowSegmentAbsorbsOverflow(t *testing.T) {
	st := theme.BuildStyles(theme.Get("tokyonight"))
	cfg := config.StatuslineConfig{
		Left:  []config.Segment{{Type: "text", Source: "title", Grow: true, MaxWidth: 60}},
		Right: []config.Segment{{Type: "age"}},
	}
	rv := RowView{MR: core.MR{
		Title:     strings.Repeat("long title ", 10),
		UpdatedAt: time.Now().Add(-2 * time.Hour),
	}}
	const width = 40
	line := Render(cfg, st, rv, width, false)
	if w := lipgloss.Width(line); w > width {
		t.Errorf("row width %d exceeds terminal width %d", w, width)
	}
	if !strings.Contains(line, "…") {
		t.Error("grow segment should be truncated with an ellipsis")
	}
}

func TestSegmentUnknownStyleNameKeepsDefault(t *testing.T) {
	st := theme.BuildStyles(theme.Get("tokyonight"))
	seg := config.Segment{Type: "text", Source: "title", Style: "nope"}
	rv := RowView{MR: core.MR{Title: "hello"}}
	got := renderSegment(seg, st, rv, st.Base)
	want := st.Base.Inline(true).Render("hello")
	if got != want {
		t.Errorf("unknown style should keep default:\n got %q\nwant %q", got, want)
	}
}
