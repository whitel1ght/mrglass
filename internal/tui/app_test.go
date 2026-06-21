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
