package watch

import (
	"path/filepath"
	"testing"

	"github.com/dmitry/mrglass/internal/config"
	"github.com/dmitry/mrglass/internal/core"
)

type fakeProvider struct{ mrs []core.MR }

func (f fakeProvider) Whoami() (string, error) { return "you", nil }
func (f fakeProvider) List(string, int, string) ([]core.MR, error) {
	return f.mrs, nil
}

func mineMR(ref, ci string) core.MR {
	m := core.MR{Ref: ref, Title: "t", URL: "u", Role: core.RoleMine, CI: ci, TicketKey: "ABC-1"}
	m.SetApprovalsOK(true)
	return m
}

func TestFetchFirstRunNoChanges(t *testing.T) {
	d := Deps{
		Provider:  fakeProvider{mrs: []core.MR{mineMR("g/p!1", "success")}},
		Me:        "you",
		StatePath: filepath.Join(t.TempDir(), "state.json"),
		Cfg:       config.Default(),
	}
	res := Fetch(d)
	if res.Err != nil {
		t.Fatalf("err: %v", res.Err)
	}
	if len(res.Changes) != 0 {
		t.Errorf("first run should report no changes, got %v", res.Changes)
	}
	if len(res.MRs) != 1 {
		t.Errorf("want 1 MR, got %d", len(res.MRs))
	}
}

func TestFetchDetectsCIChangeOnSecondRun(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	d1 := Deps{Provider: fakeProvider{mrs: []core.MR{mineMR("g/p!1", "success")}},
		Me: "you", StatePath: statePath, Cfg: config.Default()}
	Fetch(d1) // baseline

	d2 := Deps{Provider: fakeProvider{mrs: []core.MR{mineMR("g/p!1", "failed")}},
		Me: "you", StatePath: statePath, Cfg: config.Default()}
	res := Fetch(d2)
	if len(res.Changes) != 1 || res.Changes[0].Kind != core.KindChanged {
		t.Fatalf("want 1 changed, got %+v", res.Changes)
	}
}

func TestTriageWorthyFilters(t *testing.T) {
	changes := []core.Change{
		{Kind: core.KindChanged, Fields: []core.FieldChange{{Field: "ci", New: "failed"}}},
		{Kind: core.KindChanged, Fields: []core.FieldChange{{Field: "comments", Old: 1, New: 2}}},
	}
	got := TriageWorthy(changes)
	if len(got) != 1 {
		t.Errorf("only the CI-failed change is worthy, got %d", len(got))
	}
}
