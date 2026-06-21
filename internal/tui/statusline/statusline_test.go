package statusline

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dmitry/mrglass/internal/config"
	"github.com/dmitry/mrglass/internal/core"
	"github.com/dmitry/mrglass/internal/tui/theme"
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

func TestRenderIncludesTitleAndCISymbol(t *testing.T) {
	out := Render(cfg(), theme.BuildStyles(theme.Get("default")), baseRow(), 80, false)
	if !strings.Contains(out, "feat: thing") {
		t.Errorf("row should contain the title: %q", out)
	}
	if !strings.Contains(out, "✗") {
		t.Errorf("failed CI should render ✗: %q", out)
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
