package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/whitel1ght/mrglass/internal/analyze"
	"github.com/whitel1ght/mrglass/internal/config"
	"github.com/whitel1ght/mrglass/internal/core"
	jiraPkg "github.com/whitel1ght/mrglass/internal/jira"
	"github.com/whitel1ght/mrglass/internal/review"
	"github.com/whitel1ght/mrglass/internal/watch"
)

// drain executes a tea.Cmd, recursively flattening tea.Batch, so tests can
// exercise the real work command even when it is batched with a spinner tick.
func drain(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			drain(c)
		}
	}
}

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

func (f *fakeReviewGL) MRDiff(core.MR) (string, error) { return "a diff", nil }
func (f *fakeReviewGL) PostNote(_ core.MR, body string) error {
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
	drain(cmd)
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

func TestFetchWarningShownInStatus(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 30
	updated, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: nil, Warning: "state save failed: disk full"}))
	view := updated.(Model).View()
	if !strings.Contains(view, "state save failed") {
		t.Errorf("fetch warning should appear in the status line:\n%s", view)
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

func TestSkillNotFiredDoesNotEnterConfirm(t *testing.T) {
	// reviewSkill is set (default config has it? no — set it explicitly).
	gl := &fakeReviewGL{}
	m := newTestModel().WithReview(fakeReviewer{text: "flat ad-hoc review"}, gl)
	m.cfg.ReviewSkill = "claude-components:mr-review-multi-agent"
	m.width, m.height = 100, 30
	item := mr("g/p!1", "success")
	item.Role = core.RoleReviewRequested
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = u.(Model)
	m, _ = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	// result arrives WITHOUT a skill having fired (SkillsUsed empty)
	m, _ = update(m, reviewMsg(review.Result{Ref: "g/p!1", Text: "flat ad-hoc review"}))
	if m.pendingReview != nil {
		t.Error("a configured skill that didn't fire must NOT enter the confirm/post flow")
	}
	if !strings.Contains(m.status, "did not run") {
		t.Errorf("status should flag the skill didn't run + retry, got %q", m.status)
	}
}

func TestSkillFiredEntersConfirm(t *testing.T) {
	// sanity: when the skill DID fire, the flow proceeds normally.
	gl := &fakeReviewGL{}
	m := newTestModel().WithReview(fakeReviewer{text: "structured review"}, gl)
	m.cfg.ReviewSkill = "claude-components:mr-review-multi-agent"
	m.width, m.height = 100, 30
	item := mr("g/p!1", "success")
	item.Role = core.RoleReviewRequested
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = u.(Model)
	m, _ = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m, _ = update(m, reviewMsg(review.Result{Ref: "g/p!1", Text: "structured review",
		SkillsUsed: []string{"claude-components:mr-review-multi-agent"}}))
	if m.pendingReview == nil {
		t.Error("when the skill fired, the review should enter the confirm flow")
	}
}

// flakyGL fails PostNote the first N times, then succeeds — to test retry.
type flakyGL struct {
	failsLeft  int
	postCalled int
}

func (flakyGL) MRDiff(core.MR) (string, error) { return "a diff", nil }
func (f *flakyGL) PostNote(_ core.MR, body string) error {
	f.postCalled++
	if f.failsLeft > 0 {
		f.failsLeft--
		return fmt.Errorf("transient: connection reset")
	}
	return nil
}

func TestPostFailureKeepsReviewForRetry(t *testing.T) {
	gl := &flakyGL{failsLeft: 1}
	m := newTestModel().WithReview(fakeReviewer{text: "the review"}, gl)
	m.cfg.ReviewSkill = "" // no skill so the result path enters confirm directly
	m.width, m.height = 100, 30
	item := mr("g/p!1", "success")
	item.Role = core.RoleReviewRequested
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = u.(Model)
	m, _ = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m, _ = update(m, reviewMsg(review.Result{Ref: "g/p!1", Text: "the review"}))
	if m.pendingReview == nil {
		t.Fatal("should be in confirm state")
	}

	// First y -> post fails -> review MUST be kept for retry, status says retry
	m, cmd := update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd != nil {
		cmd() // run the (failing) post
	}
	m, _ = update(m, postResultMsg{ref: "g/p!1", err: fmt.Errorf("transient: connection reset")})
	if m.pendingReview == nil {
		t.Error("a failed post must KEEP the pending review for retry")
	}
	if !strings.Contains(m.status, "retry") {
		t.Errorf("status should offer retry, got %q", m.status)
	}

	// Second y -> post succeeds -> review cleared, success status
	m, cmd = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd != nil {
		cmd()
	}
	m, _ = update(m, postResultMsg{ref: "g/p!1", err: nil})
	if m.pendingReview != nil {
		t.Error("a successful post should clear the pending review")
	}
	if !strings.Contains(m.status, "posted") {
		t.Errorf("status should confirm posted, got %q", m.status)
	}
}

func TestOpenTicketNoBaseURL(t *testing.T) {
	m := newTestModel()
	m.cfg.Tickets.URLTemplate = "" // not configured
	m.width, m.height = 100, 30
	item := mr("g/p!1", "success")
	item.Role = core.RoleReviewRequested
	item.TicketKey = "PROJ-1234"
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = u.(Model)
	u2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")})
	if cmd != nil {
		t.Error("J with no baseURL should not open anything")
	}
	if !strings.Contains(u2.(Model).status, "tickets.urlTemplate") {
		t.Errorf("status should prompt to configure urlTemplate, got %q", u2.(Model).status)
	}
}

func TestOpenTicketNoTicket(t *testing.T) {
	m := newTestModel()
	m.cfg.Tickets.URLTemplate = "https://acme.atlassian.net/browse/{key}"
	m.width, m.height = 100, 30
	item := mr("g/p!1", "success")
	item.Role = core.RoleReviewRequested
	item.TicketKey = "Other" // no ticket
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = u.(Model)
	u2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")})
	if cmd != nil {
		t.Error("J on a no-ticket MR should not open anything")
	}
	if !strings.Contains(u2.(Model).status, "no ticket") {
		t.Errorf("status should note no ticket, got %q", u2.(Model).status)
	}
}

func TestOpenTicketOpens(t *testing.T) {
	m := newTestModel()
	m.cfg.Tickets.URLTemplate = "https://acme.atlassian.net/browse/{key}"
	m.width, m.height = 100, 30
	item := mr("g/p!1", "success")
	item.Role = core.RoleReviewRequested
	item.TicketKey = "PROJ-1234"
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = u.(Model)
	u2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")})
	if cmd == nil {
		t.Error("J with baseURL + ticket should return an open command")
	}
	if !strings.Contains(u2.(Model).status, "PROJ-1234") {
		t.Errorf("status should note the ticket opening, got %q", u2.(Model).status)
	}
}

// fakeJira implements jira.Client for tests.
type fakeJira struct {
	t       jiraPkg.Ticket
	err     error
	calls   int
	lastKey string
}

func (f *fakeJira) Fetch(key string) (jiraPkg.Ticket, error) {
	f.calls++
	f.lastKey = key
	return f.t, f.err
}

func jiraMR() core.MR {
	m := mr("g/p!1", "success")
	m.Role = core.RoleReviewRequested
	m.TicketKey = "PROJ-1234"
	return m
}

func TestExpandFetchesTicketWhenConfigured(t *testing.T) {
	fj := &fakeJira{t: jiraPkg.Ticket{Key: "PROJ-1234", Status: "In Review", StatusCategory: "indeterminate", Assignee: "Jane"}}
	m := newTestModel().WithJira(fj)
	m.width, m.height = 100, 30
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{jiraMR()}}))
	m = u.(Model)

	// expand -> should return a fetch cmd
	m2, cmd := update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expanding a ticketed MR with Jira configured should fetch")
	}
	drain(cmd) // runs Fetch
	if fj.calls != 1 || fj.lastKey != "PROJ-1234" {
		t.Errorf("Fetch should be called once with the ticket key, got calls=%d key=%q", fj.calls, fj.lastKey)
	}
	// deliver the result and check the detail shows it
	m3, _ := update(m2, jiraMsg{key: "PROJ-1234", ticket: fj.t})
	if !strings.Contains(m3.View(), "In Review") {
		t.Errorf("expanded detail should show the ticket status:\n%s", m3.View())
	}
}

func TestExpandDoesNotRefetchCachedTicket(t *testing.T) {
	fj := &fakeJira{t: jiraPkg.Ticket{Key: "PROJ-1234", Status: "Done", StatusCategory: "done"}}
	m := newTestModel().WithJira(fj)
	m.width, m.height = 100, 30
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{jiraMR()}}))
	m = u.(Model)
	m, cmd := update(m, tea.KeyMsg{Type: tea.KeyEnter}) // expand -> fetch
	if cmd != nil {
		cmd()
	}
	m, _ = update(m, jiraMsg{key: "PROJ-1234", ticket: fj.t}) // cache it
	callsAfterFirst := fj.calls
	// collapse + re-expand -> must NOT refetch (cached & fresh)
	m, _ = update(m, tea.KeyMsg{Type: tea.KeyEnter})     // collapse
	_, cmd2 := update(m, tea.KeyMsg{Type: tea.KeyEnter}) // re-expand
	if cmd2 != nil {
		t.Error("re-expanding a cached, fresh ticket must not refetch")
	}
	if fj.calls != callsAfterFirst {
		t.Errorf("no extra fetch expected, calls went %d -> %d", callsAfterFirst, fj.calls)
	}
}

func TestExpandNoFetchWhenJiraUnconfigured(t *testing.T) {
	m := newTestModel() // no WithJira
	m.width, m.height = 100, 30
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{jiraMR()}}))
	m = u.(Model)
	_, cmd := update(m, tea.KeyMsg{Type: tea.KeyEnter}) // expand
	if cmd != nil {
		t.Error("expand must not fetch when Jira is unconfigured")
	}
	// detail must not contain a ticket line
	if strings.Contains(m.View(), "🎫") {
		t.Errorf("no ticket line when Jira unconfigured:\n%s", m.View())
	}
}

func TestExpandNoFetchWhenNoTicket(t *testing.T) {
	fj := &fakeJira{}
	m := newTestModel().WithJira(fj)
	m.width, m.height = 100, 30
	item := jiraMR()
	item.TicketKey = "Other" // no ticket
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = u.(Model)
	_, cmd := update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || fj.calls != 0 {
		t.Errorf("MR with no ticket must not fetch (cmd=%v calls=%d)", cmd != nil, fj.calls)
	}
}

func TestJiraDisabledShowsReasonOnExpand(t *testing.T) {
	// status: jira requested but no token → WithJiraDisabled → expanded ticketed
	// MR shows the reason, not nothing.
	m := newTestModel().WithJiraDisabled("status off: set JIRA_EMAIL + JIRA_API_TOKEN")
	m.width, m.height = 100, 40
	item := jiraMR() // has TicketKey PROJ-1234, role review_requested
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = u.(Model)
	// expand — must NOT fetch (no client) and must show the reason
	_, cmd := update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("disabled jira must not issue a fetch on expand")
	}
	m2, _ := update(m, tea.KeyMsg{Type: tea.KeyEnter})  // collapse
	m3, _ := update(m2, tea.KeyMsg{Type: tea.KeyEnter}) // expand again
	v := m3.View()
	if !strings.Contains(v, "PROJ-1234") || !strings.Contains(v, "JIRA_EMAIL") {
		t.Errorf("expanded detail should explain jira is off:\n%s", v)
	}
}

func TestNoJiraNoteWhenStatusNotRequested(t *testing.T) {
	// jira nil AND no note (status: none) → expanded ticketed MR shows no 🎫 line
	m := newTestModel() // no WithJira / WithJiraDisabled
	m.width, m.height = 100, 40
	item := jiraMR()
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = u.(Model)
	m, _ = update(m, tea.KeyMsg{Type: tea.KeyEnter}) // expand
	if strings.Contains(m.View(), "🎫") {
		t.Errorf("no ticket line when status not requested:\n%s", m.View())
	}
}

func TestOpenWorkNoOpenCommand(t *testing.T) {
	m := newTestModel()
	m.cfg.Worktree.OpenCommand = "" // not configured
	m.width, m.height = 100, 30
	item := jiraMR()
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = u.(Model)
	u2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	if cmd != nil {
		t.Error("w with no openCommand should not run anything")
	}
	if !strings.Contains(u2.(Model).status, "worktree.openCommand") {
		t.Errorf("status should prompt to configure openCommand, got %q", u2.(Model).status)
	}
}

func TestOpenWorkNoLocalClone(t *testing.T) {
	m := newTestModel()
	m.cfg.Worktree.OpenCommand = "tmux new-window -c {dir} {cmd}"
	m.cfg.ProjectsDir = t.TempDir() // exists but has no matching clone
	m.width, m.height = 100, 30
	item := jiraMR()
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{item}}))
	m = u.(Model)
	u2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	if cmd != nil {
		t.Error("w with no local clone should not run anything")
	}
	if !strings.Contains(u2.(Model).status, "no local clone") {
		t.Errorf("status should note no local clone, got %q", u2.(Model).status)
	}
}

func TestViewHasBlankLineBelowTabs(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	lines := strings.Split(m.View(), "\n")
	if len(lines) < 2 {
		t.Fatalf("view too short: %q", lines)
	}
	if strings.TrimSpace(lines[1]) != "" {
		t.Errorf("line below the tab bar should be empty for separation, got %q", lines[1])
	}
	// The view must still fill exactly the terminal height.
	if len(lines) != 24 {
		t.Errorf("view height %d, want %d", len(lines), 24)
	}
}

func TestBusySpinnerVisibleWhileReviewing(t *testing.T) {
	m, _ := reviewModel(t)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = u.(Model)
	view := m.View()
	if !strings.Contains(view, m.spinner.View()) {
		t.Errorf("footer should show an animated spinner while reviewing:\n%s", view)
	}
	if !strings.Contains(view, "reviewing g/p!1") {
		t.Errorf("footer should name the in-flight operation:\n%s", view)
	}
	// A failed review returns to the list view; the busy indicator must be gone.
	u, _ = m.Update(reviewMsg(review.Result{Ref: "g/p!1", Err: errors.New("boom")}))
	m = u.(Model)
	view = m.View()
	if strings.Contains(view, m.spinner.View()) || strings.Contains(view, "reviewing g/p!1") {
		t.Errorf("busy indicator should clear once the review finishes:\n%s", view)
	}
}

func TestBusySpinnerVisibleWhileRefreshing(t *testing.T) {
	m, _ := reviewModel(t)
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = u.(Model)
	if cmd == nil {
		t.Fatal("'r' should return a command")
	}
	if view := m.View(); !strings.Contains(view, m.spinner.View()) || !strings.Contains(view, "refreshing") {
		t.Errorf("footer should show spinner + refreshing label:\n%s", view)
	}
	u, _ = m.Update(fetchResultMsg(watch.FetchResult{MRs: nil}))
	m = u.(Model)
	if view := m.View(); strings.Contains(view, m.spinner.View()) {
		t.Errorf("spinner should clear after the fetch result arrives:\n%s", view)
	}
}

func TestSpinnerTickStopsWhenIdle(t *testing.T) {
	m, _ := reviewModel(t)
	if _, cmd := m.Update(spinner.TickMsg{}); cmd != nil {
		t.Error("spinner ticks while idle should not reschedule (animation must stop)")
	}
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = u.(Model)
	if _, cmd := m.Update(spinner.TickMsg{}); cmd == nil {
		t.Error("spinner ticks while busy should reschedule to keep animating")
	}
}

func TestTriageErrorSurfacedInStatus(t *testing.T) {
	m, _ := reviewModel(t)
	u, _ := m.Update(adviceMsg(analyze.Advice{Ref: "g/p!1", Err: errors.New("claude: Not logged in")}))
	m = u.(Model)
	view := m.View()
	if !strings.Contains(view, "triage") || !strings.Contains(view, "Not logged in") {
		t.Errorf("a failed triage must be visible in the status footer, got:\n%s", view)
	}
}

// hideModel builds a model with a real temp state path (hide persists to a
// sibling file) and two review_requested MRs in the default first section.
func hideModel(t *testing.T) (Model, string) {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "state.json")
	m := New(config.Default(), nil, "you", nil, statePath)
	m.width, m.height = 120, 30
	one := mr("g/p!1", "success")
	one.Title, one.Role = "one", core.RoleReviewRequested
	two := mr("g/p!2", "success")
	two.Title, two.Role = "two", core.RoleReviewRequested
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{one, two}}))
	return u.(Model), statePath
}

func TestBackspaceHidesAndHiddenTabAppears(t *testing.T) {
	m, _ := hideModel(t)
	if strings.Contains(m.View(), "Hidden (") {
		t.Fatal("no hidden tab before anything is hidden")
	}
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace}) // hide "one" (cursor 0)
	m = u.(Model)
	view := m.View()
	if strings.Contains(view, "▸ ") && strings.Contains(view, " one") && !strings.Contains(view, "Hidden") {
		t.Errorf("hidden MR should leave the section:\n%s", view)
	}
	if !strings.Contains(view, "Hidden (1)") {
		t.Errorf("hidden tab with count should appear:\n%s", view)
	}
	// Cycle to the hidden tab (3 configured sections -> hidden is index 3).
	for i := 0; i < 3; i++ {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = u.(Model)
	}
	view = m.View()
	if !strings.Contains(view, "[Hidden (1)]") {
		t.Fatalf("should land on the hidden tab:\n%s", view)
	}
	if !strings.Contains(view, "one") {
		t.Errorf("hidden tab should list the hidden MR:\n%s", view)
	}
	// Backspace on the hidden tab restores; the tab disappears.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = u.(Model)
	view = m.View()
	if strings.Contains(view, "Hidden (") {
		t.Errorf("hidden tab should disappear once empty:\n%s", view)
	}
}

func TestHiddenPersistsAcrossRestart(t *testing.T) {
	m, statePath := hideModel(t)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace}) // hide "one"
	m = u.(Model)
	if _, err := os.Stat(core.HiddenPath(statePath)); err != nil {
		t.Fatalf("hidden file should be written: %v", err)
	}
	// "Restart": a fresh model on the same state path sees the MR as hidden.
	m2 := New(config.Default(), nil, "you", nil, statePath)
	m2.width, m2.height = 120, 30
	one := mr("g/p!1", "success")
	one.Title, one.Role = "one", core.RoleReviewRequested
	u2, _ := m2.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{one}}))
	m2 = u2.(Model)
	if !strings.Contains(m2.View(), "Hidden (1)") {
		t.Errorf("hidden set should survive restart:\n%s", m2.View())
	}
}

func TestCopyReviewInConfirmMode(t *testing.T) {
	var copied string
	orig := clipboardRun
	clipboardRun = func(text string) error { copied = text; return nil }
	t.Cleanup(func() { clipboardRun = orig })

	m, _ := reviewModel(t)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}) // start review
	m = u.(Model)
	u, _ = m.Update(reviewMsg(review.Result{Ref: "g/p!1", Text: "LGTM, ship it"}))
	m = u.(Model)
	if m.pendingReview == nil {
		t.Fatal("expected confirm state")
	}
	if !strings.Contains(m.View(), "[c]opy") {
		t.Errorf("confirm prompt should advertise copy:\n%s", m.View())
	}
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}) // copy
	m = u.(Model)
	if cmd == nil {
		t.Fatal("'c' in confirm mode should return a copy command")
	}
	u, _ = m.Update(cmd()) // run the copy, deliver its result
	m = u.(Model)
	if copied != "LGTM, ship it" {
		t.Errorf("clipboard got %q, want the review text", copied)
	}
	if m.pendingReview == nil {
		t.Error("copying must stay in the confirm view")
	}
	if !strings.Contains(m.View(), "review copied") {
		t.Errorf("status should confirm the copy:\n%s", m.View())
	}
}

func TestCopyReviewFailureSurfaces(t *testing.T) {
	orig := clipboardRun
	clipboardRun = func(string) error { return errors.New("no pbcopy") }
	t.Cleanup(func() { clipboardRun = orig })

	m, _ := reviewModel(t)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = u.(Model)
	u, _ = m.Update(reviewMsg(review.Result{Ref: "g/p!1", Text: "body"}))
	m = u.(Model)
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = u.(Model)
	u, _ = m.Update(cmd())
	m = u.(Model)
	if !strings.Contains(m.View(), "copy failed") {
		t.Errorf("copy failure should surface in status:\n%s", m.View())
	}
	if m.pendingReview == nil {
		t.Error("a failed copy must not discard the pending review")
	}
}

func TestHelpOverlayIsGroupedAndBordered(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 120, 40
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = u.(Model)
	view := m.View()
	for _, want := range []string{"Navigation", "Actions", "App", "mrglass"} {
		if !strings.Contains(view, want) {
			t.Errorf("help overlay missing %q:\n%s", want, view)
		}
	}
	// every full-help key should be listed
	for _, want := range []string{"enter", "⌫", "claude review", "hide/unhide"} {
		if !strings.Contains(view, want) {
			t.Errorf("help overlay missing binding %q", want)
		}
	}
	// bordered (rounded box drawing char)
	if !strings.ContainsAny(view, "╭╮╰╯─│") {
		t.Errorf("help overlay should be bordered:\n%s", view)
	}
	// esc closes it
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	if m.showHelp {
		t.Error("esc should close the help overlay")
	}
}

// projModel builds a loaded model with MRs across three projects, all in the
// default first section (review_requested).
func projModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel()
	m.width, m.height = 140, 40
	mk := func(ref string) core.MR {
		x := mr(ref, "success")
		x.Role = core.RoleReviewRequested
		return x
	}
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{
		mk("acme/api!1"), mk("acme/api!2"), mk("acme/web!3"), mk("infra/deploy!4"),
	}}))
	return u.(Model)
}

func TestProjectRowListsDistinctSortedWithAll(t *testing.T) {
	m := projModel(t)
	// The project row is the header line beginning with the "[ ]" hint.
	var row string
	for _, ln := range strings.Split(m.View(), "\n") {
		if strings.Contains(ln, "[/]") {
			row = ln
			break
		}
	}
	if row == "" {
		t.Fatalf("no project row found:\n%s", m.View())
	}
	// Projects sort by full path (acme/api < acme/web < infra/deploy), so the
	// short-name labels appear in the order api, web, deploy after All.
	iAll := strings.Index(row, "All")
	iApi := strings.Index(row, "api")
	iWeb := strings.Index(row, "web")
	iDeploy := strings.Index(row, "deploy")
	if iAll < 0 || iApi < 0 || iWeb < 0 || iDeploy < 0 {
		t.Fatalf("project row missing entries: %q", row)
	}
	if !(iAll < iApi && iApi < iWeb && iWeb < iDeploy) {
		t.Errorf("project row order wrong (want All<api<web<deploy): %q", row)
	}
	// The [ ]-key hint sits at the END of the row, after the tabs — no leading
	// "[ ]" artifact that looks like an empty tab.
	if !strings.Contains(row, "[/]") {
		t.Errorf("project row should carry a key hint: %q", row)
	}
	if strings.Index(row, "[/]") < iDeploy {
		t.Errorf("key hint should follow the project tabs, not lead them: %q", row)
	}
}

func TestProjectRowHiddenWhenSingleProject(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 140, 40
	one := mr("acme/api!1", "success")
	one.Role = core.RoleReviewRequested
	u, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{one}}))
	m = u.(Model)
	for _, ln := range strings.Split(m.View(), "\n") {
		if strings.Contains(ln, "[/]") {
			t.Errorf("project row should be absent with a single project: %q", ln)
		}
	}
}

func TestProjectFilterCyclesAndFilters(t *testing.T) {
	m := projModel(t)
	if m.projectFilter != "" {
		t.Fatalf("default should be All, got %q", m.projectFilter)
	}
	// ] -> first project (acme/api, alphabetical)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	m = u.(Model)
	if m.projectFilter != "acme/api" {
		t.Fatalf("after ] want acme/api, got %q", m.projectFilter)
	}
	if len(m.mrs) != 2 {
		t.Errorf("acme/api should show 2 MRs, got %d", len(m.mrs))
	}
	for _, x := range m.mrs {
		if x.Project() != "acme/api" {
			t.Errorf("list contains non-api MR %q", x.Ref)
		}
	}
	// [ back to All
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	m = u.(Model)
	if m.projectFilter != "" {
		t.Errorf("[ from first project should wrap to All, got %q", m.projectFilter)
	}
	if len(m.mrs) != 4 {
		t.Errorf("All should show 4 MRs, got %d", len(m.mrs))
	}
}

func TestProjectFilterPersistsAcrossStatusTab(t *testing.T) {
	m := projModel(t)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")}) // acme/api
	m = u.(Model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab}) // next status section
	m = u.(Model)
	if m.projectFilter != "acme/api" {
		t.Errorf("project filter should persist across status tabs, got %q", m.projectFilter)
	}
}

func TestProjectFilterVanishedFallsBackToAll(t *testing.T) {
	m := projModel(t)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")}) // acme/api
	m = u.(Model)
	// refresh with acme/api gone
	web := mr("acme/web!3", "success")
	web.Role = core.RoleReviewRequested
	u, _ = m.Update(fetchResultMsg(watch.FetchResult{MRs: []core.MR{web}}))
	m = u.(Model)
	if m.projectFilter != "" {
		t.Errorf("vanished project should fall back to All, got %q", m.projectFilter)
	}
}
