package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dmitry/mrglass/internal/analyze"
	"github.com/dmitry/mrglass/internal/config"
	"github.com/dmitry/mrglass/internal/core"
	"github.com/dmitry/mrglass/internal/review"
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

// fake review reviewer + gitlab for the review-flow tests.
type fakeReviewer struct{ text string }

func (f fakeReviewer) Review(req review.ReviewReq) review.Result {
	return review.Result{Ref: req.MR.Ref, Text: f.text}
}

type fakeReviewGL struct {
	posted     string
	postCalled bool
}

func (f *fakeReviewGL) MRDiff(int, int) (string, error) { return "a diff", nil }
func (f *fakeReviewGL) PostNote(_, _ int, body string) error {
	f.postCalled, f.posted = true, body
	return nil
}

// reviewModel builds a model with a selectable approved-mine MR (matches the
// default first "Mine"-style section is index 1; use review_requested for index 0)
// and the review feature wired.
func reviewModel(t *testing.T) (Model, *fakeReviewGL) {
	t.Helper()
	gl := &fakeReviewGL{}
	m := newTestModel().WithReview(fakeReviewer{text: "LGTM, ship it"}, gl)
	m.width, m.height = 120, 30
	item := mr("g/p!1", "success")
	item.Role = core.RoleReviewRequested // matches default section 0
	updated, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	return updated.(Model), gl
}

func TestReviewFlowConfirmAndPost(t *testing.T) {
	m, gl := reviewModel(t)

	// Press 'c' -> kicks off a review (reviewing flag set, command returned).
	rmodel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = rmodel.(Model)
	if cmd == nil || !m.reviewing {
		t.Fatal("'c' should start a review")
	}

	// Simulate the review result arriving.
	rmodel, _ = m.Update(reviewMsg(review.Result{Ref: "g/p!1", Text: "LGTM, ship it"}))
	m = rmodel.(Model)
	if m.pendingReview == nil {
		t.Fatal("review result should enter confirm state")
	}
	if v := m.View(); !strings.Contains(v, "LGTM, ship it") || !strings.Contains(v, "post") {
		t.Errorf("confirm view should show review + post prompt:\n%s", v)
	}

	// Press 'y' -> posts.
	rmodel, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = rmodel.(Model)
	if cmd == nil {
		t.Fatal("'y' should return a post command")
	}
	// Run the returned command to exercise the post.
	cmd()
	if !gl.postCalled || gl.posted != "LGTM, ship it" {
		t.Errorf("post not performed: called=%v body=%q", gl.postCalled, gl.posted)
	}
}

func TestReviewFlowDiscard(t *testing.T) {
	m, gl := reviewModel(t)
	m, _ = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m, _ = update(m, reviewMsg(review.Result{Ref: "g/p!1", Text: "some review"}))
	if m.pendingReview == nil {
		t.Fatal("should be in confirm state")
	}
	// Press 'n' -> discards, nothing posted.
	m, _ = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.pendingReview != nil {
		t.Error("'n' should clear the pending review")
	}
	if gl.postCalled {
		t.Error("'n' must NOT post anything")
	}
}

func TestReviewUnavailableWithoutClaude(t *testing.T) {
	// No WithReview wiring -> 'c' must not crash and must report unavailable.
	m := newTestModel()
	m.width, m.height = 120, 30
	item := mr("g/p!1", "success")
	item.Role = core.RoleReviewRequested
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = u.(Model)
	u2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if cmd != nil {
		t.Error("'c' with no reviewer should not start a review")
	}
	if !strings.Contains(u2.(Model).status, "unavailable") {
		t.Errorf("status should note review unavailable, got %q", u2.(Model).status)
	}
}

// update is a tiny helper to thread Model through Update in tests.
func update(m Model, msg tea.Msg) (Model, tea.Cmd) {
	nm, cmd := m.Update(msg)
	return nm.(Model), cmd
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
	// matching section 1 ("Mine · Approved": role==mine && approved).
	review := mr("g/p!1", "success")
	review.Role = core.RoleReviewRequested
	review.Title = "review me"
	mine := mr("g/p!2", "success")
	mine.Role = core.RoleMine
	mine.ApprovedBy = []string{"alice"} // approved -> matches "Mine · Approved"
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

func TestTabsShowCounts(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 120, 30
	// 2 review MRs (section 0), 1 approved-mine (section 1).
	r1 := mr("g/p!1", "success")
	r1.Role = core.RoleReviewRequested
	r2 := mr("g/p!2", "success")
	r2.Role = core.RoleReviewRequested
	mineApproved := mr("g/p!3", "success")
	mineApproved.Role = core.RoleMine
	mineApproved.ApprovedBy = []string{"alice"}
	updated, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{r1, r2, mineApproved}}))
	v := updated.(Model).View()
	// "Needs My Review (2)" and "Mine · Approved (1)" should appear.
	if !strings.Contains(v, "Needs My Review (2)") {
		t.Errorf("review tab should show count (2):\n%s", v)
	}
	if !strings.Contains(v, "Mine · Approved (1)") {
		t.Errorf("approved-mine tab should show count (1):\n%s", v)
	}
}

func TestTabsHaveNoCountBeforeLoad(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 120, 30
	// Before any fetch, tabs must not show "(0)" — counts only appear once loaded.
	if strings.Contains(m.View(), "(0)") {
		t.Errorf("tabs should not show counts before the first load:\n%s", m.View())
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

func TestReviewConfirmIsScrollableNotTruncated(t *testing.T) {
	// A review taller than the terminal must be reachable by scrolling — the old
	// fixed-height box silently truncated everything past the first screen.
	gl := &fakeReviewGL{}
	// build a long, sectioned review whose later sections are off the first screen
	var b strings.Builder
	b.WriteString("## Summary\nintro paragraph\n")
	for i := 0; i < 80; i++ {
		b.WriteString(fmt.Sprintf("filler line %d\n", i))
	}
	b.WriteString("## Blockers\nB1 — the important blocker\n")
	long := b.String()
	m := newTestModel().WithReview(fakeReviewer{text: long}, gl)
	m.width, m.height = 100, 24 // body ~21 lines; review ~83 lines

	item := mr("g/p!1", "success")
	item.Role = core.RoleReviewRequested
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = u.(Model)
	m, _ = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m, _ = update(m, reviewMsg(review.Result{Ref: "g/p!1", Text: long}))
	if m.pendingReview == nil {
		t.Fatal("should be in confirm state")
	}

	// Top of the review is visible; the Blockers section (near the bottom) is NOT
	// yet visible in the initial viewport.
	top := m.View()
	if !strings.Contains(top, "## Summary") {
		t.Errorf("top of review should be visible:\n%s", top)
	}
	if strings.Contains(top, "the important blocker") {
		t.Skip("terminal tall enough to show all; scroll not exercised")
	}

	// Scroll to the bottom — the Blockers section must become reachable.
	m, _ = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if !strings.Contains(m.View(), "the important blocker") {
		t.Errorf("after scrolling, the Blockers section must be reachable:\n%s", m.View())
	}
}

func TestScrollKeyDoesNotPostOrDiscard(t *testing.T) {
	gl := &fakeReviewGL{}
	m := newTestModel().WithReview(fakeReviewer{text: "## Blockers\nB1\n"}, gl)
	m.width, m.height = 100, 24
	item := mr("g/p!1", "success")
	item.Role = core.RoleReviewRequested
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = u.(Model)
	m, _ = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m, _ = update(m, reviewMsg(review.Result{Ref: "g/p!1", Text: "## Blockers\nB1\n"}))
	// pressing j (scroll) must NOT post and must NOT leave confirm state
	m, _ = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if m.pendingReview == nil {
		t.Error("'j' (scroll) must not discard/post the review")
	}
	if gl.postCalled {
		t.Error("'j' must never post")
	}
}

func TestReviewContentWrapsNotClipped(t *testing.T) {
	gl := &fakeReviewGL{}
	// one very long line, far wider than the terminal
	long := "## Important\n" + strings.Repeat("verylongwordsegment ", 40) + "\nend marker"
	m := newTestModel().WithReview(fakeReviewer{text: long}, gl)
	m.width, m.height = 60, 40 // narrow so wrapping is forced, tall so it all fits
	item := mr("g/p!1", "success")
	item.Role = core.RoleReviewRequested
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = u.(Model)
	m, _ = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m, _ = update(m, reviewMsg(review.Result{Ref: "g/p!1", Text: long}))

	// Check the scrollable review body specifically (not the fixed footer): no
	// content line may exceed the viewport width — it must wrap, not clip.
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	for _, ln := range strings.Split(m.reviewVP.View(), "\n") {
		clean := ansi.ReplaceAllString(ln, "")
		if len([]rune(clean)) > m.reviewVP.Width {
			t.Errorf("review line exceeds viewport width %d (not wrapped): %q", m.reviewVP.Width, clean)
		}
	}
	// and the long content must still be present (wrapped, not dropped)
	full := ansi.ReplaceAllString(m.reviewVP.View(), "")
	if !strings.Contains(full, "verylongwordsegment") {
		t.Errorf("wrapped content should be visible: %q", full)
	}
}
