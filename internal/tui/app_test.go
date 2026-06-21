package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dmitry/mrglass/internal/analyze"
	"github.com/dmitry/mrglass/internal/config"
	"github.com/dmitry/mrglass/internal/core"
	"github.com/dmitry/mrglass/internal/watch"
)

func mr(ref, ci string) core.MR {
	m := core.MR{Ref: ref, Title: "feat: thing", URL: "u", Role: core.RoleMine,
		CI: ci, TicketKey: "ABC-1"}
	m.SetApprovalsOK(true)
	return m
}

func newTestModel() Model {
	return New(config.Default(), nil, "you", nil, "/tmp/ignored-state.json")
}

// mockAnalyzer is a non-nil Analyzer for tests that need one.
type mockAnalyzer struct{}

func (mockAnalyzer) Triage(c core.Change) analyze.Advice {
	return analyze.Advice{Ref: c.Ref, Text: "x"}
}

func TestFetchResultPopulatesRows(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 30
	// Default section 0 filters: role == "review_requested" && !draft
	// Use RoleReviewRequested and non-draft so the MR matches the active section.
	reviewMR := mr("g/p!1", "failed")
	reviewMR.Role = core.RoleReviewRequested
	updated, _ := m.Update(fetchResultMsg(watch.FetchResult{
		MRs: []core.MR{reviewMR},
	}))
	view := updated.(Model).View()
	if !strings.Contains(view, "feat: thing") {
		t.Errorf("view should list the MR title:\n%s", view)
	}
}

func TestTabSwitchRefiltersInstantlyWithoutFetch(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 30

	// One MR matching section 0 ("Needs My Review": review_requested) and one
	// matching section 1 ("Mine": mine).
	review := mr("g/p!1", "success")
	review.Role = core.RoleReviewRequested
	review.Title = "review me"
	mine := mr("g/p!2", "success")
	mine.Role = core.RoleMine
	mine.Title = "mine to ship"

	updated, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{review, mine}}))
	m = updated.(Model)

	// Section 0 active: only the review MR shows.
	if v := m.View(); !strings.Contains(v, "review me") || strings.Contains(v, "mine to ship") {
		t.Fatalf("section 0 should show only the review MR:\n%s", v)
	}

	// Switch to the next section. This must NOT issue a fetch command (nil cmd)
	// and must immediately reflect the new section from already-fetched data.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if cmd != nil {
		t.Error("switching tabs should not trigger a fetch command (no network round-trip)")
	}
	m = next.(Model)
	if m.sectionIdx != 1 {
		t.Fatalf("expected sectionIdx 1, got %d", m.sectionIdx)
	}
	if v := m.View(); !strings.Contains(v, "mine to ship") || strings.Contains(v, "review me") {
		t.Errorf("after switch, section 1 should show only the mine MR instantly:\n%s", v)
	}
}

func TestEmptyBeforeLoadShowsLoadingNotNoMatches(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 30
	// No fetch has returned yet -> must say loading, not "No matching MRs."
	v := m.View()
	if strings.Contains(v, "No matching MRs.") {
		t.Error("before first load the empty state must not claim 'No matching MRs.'")
	}
	if !strings.Contains(v, "loading…") {
		t.Errorf("before first load the list should show a loading indicator:\n%s", v)
	}
}

func TestEnterExpandsAndCollapsesInline(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 40
	item := mr("g/p!1", "success")
	item.Role = core.RoleReviewRequested
	item.SourceBranch = "you/feature"
	item.TargetBranch = "main"
	updated, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = updated.(Model)

	// Collapsed: detail (branch line) not shown; disclosure is ▸.
	if v := m.View(); strings.Contains(v, "you/feature → main") {
		t.Errorf("collapsed row should not show detail:\n%s", v)
	}
	if v := m.View(); !strings.Contains(v, "▸") {
		t.Errorf("collapsed row should show ▸ disclosure:\n%s", v)
	}

	// Press enter -> expands: detail visible, disclosure ▾.
	exp, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = exp.(Model)
	if v := m.View(); !strings.Contains(v, "you/feature → main") {
		t.Errorf("expanded row should show the branch detail:\n%s", v)
	}
	if v := m.View(); !strings.Contains(v, "▾") {
		t.Errorf("expanded row should show ▾ disclosure:\n%s", v)
	}

	// Press enter again -> collapses.
	col, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = col.(Model)
	if v := m.View(); strings.Contains(v, "you/feature → main") {
		t.Errorf("re-collapsed row should hide the detail again:\n%s", v)
	}
}

func TestOpenKeyReturnsCommandWithoutSuspending(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 30
	// Must match the default active section (role == "review_requested") so the
	// MR is selectable.
	item := mr("g/p!1", "success")
	item.Role = core.RoleReviewRequested
	item.URL = "https://example.com/mr/1"
	updated, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = updated.(Model)
	// 'o' on a selected MR must return a (background) command, not nil.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if cmd == nil {
		t.Error("pressing 'o' on a selected MR should return an open command")
	}
}

func TestOpenErrSurfacedInStatus(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 30
	updated, _ := m.Update(openErrMsg{err: errTest{}})
	if !strings.Contains(updated.(Model).status, "could not open browser") {
		t.Errorf("open error should surface in status, got %q", updated.(Model).status)
	}
}

type errTest struct{}

func (errTest) Error() string { return "no opener" }

func TestViewFillsTerminalHeight(t *testing.T) {
	const h = 24
	m := newTestModel()
	m.width, m.height = 100, h
	reviewMR := mr("g/p!1", "failed")
	reviewMR.Role = core.RoleReviewRequested
	updated, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{reviewMR}}))
	view := updated.(Model).View()
	lines := strings.Split(view, "\n")
	if len(lines) != h {
		t.Errorf("view should fill the full terminal height: got %d lines, want %d", len(lines), h)
	}
}

func TestViewFillsHeightWhenEmpty(t *testing.T) {
	const h = 24
	m := newTestModel()
	m.width, m.height = 100, h
	// No MRs at all -> the body must still pad to full height (footer at bottom).
	updated, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: nil}))
	view := updated.(Model).View()
	if got := len(strings.Split(view, "\n")); got != h {
		t.Errorf("empty view should still fill height: got %d lines, want %d", got, h)
	}
}

func TestToggleAutoTriageKey(t *testing.T) {
	// With a non-nil analyzer, pressing 'a' from false should flip to true.
	m := New(config.Default(), nil, "you", mockAnalyzer{}, "/tmp/ignored-state.json")
	// New() sets autoTriage = cfg.AutoTriage && az != nil; cfg.AutoTriage defaults true,
	// so autoTriage starts true. Toggle to false.
	before := m.autoTriage
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if updated.(Model).autoTriage == before {
		t.Error("'a' should toggle autoTriage when analyzer is present")
	}
}

func TestToggleAutoTriageNoopWithoutAnalyzer(t *testing.T) {
	// With a nil analyzer, pressing 'a' must be a no-op: autoTriage stays false.
	m := newTestModel() // nil analyzer, autoTriage starts false (cfg.AutoTriage && nil == false)
	if m.autoTriage {
		t.Fatal("precondition: autoTriage must start false with nil analyzer")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if updated.(Model).autoTriage {
		t.Error("'a' must not enable autoTriage when no analyzer is present")
	}
}

func TestAdviceMsgStored(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 30
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{mr("g/p!1", "failed")}}))
	m = u.(Model)
	u2, _ := m.Update(adviceMsg(analyze.Advice{Ref: "g/p!1", Text: "rebase now"}))
	if u2.(Model).advice["g/p!1"] != "rebase now" {
		t.Error("advice should be stored by ref")
	}
}
