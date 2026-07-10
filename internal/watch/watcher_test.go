package watch

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/whitel1ght/mrglass/internal/config"
	"github.com/whitel1ght/mrglass/internal/core"
)

type fakeProvider struct{ mrs []core.MR }

func (f fakeProvider) Whoami() (string, error) { return "you", nil }
func (f fakeProvider) List(string, int, string) ([]core.MR, error) {
	return f.mrs, nil
}

type errProvider struct{}

func (errProvider) Whoami() (string, error) { return "you", nil }
func (errProvider) List(string, int, string) ([]core.MR, error) {
	return nil, errors.New("glab boom")
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

func TestFetchProviderErrorReturnsErrAndDoesNotWriteState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	d := Deps{
		Provider:  errProvider{},
		Me:        "you",
		StatePath: statePath,
		Cfg:       config.Default(),
	}
	res := Fetch(d)

	// Assert error is returned
	if res.Err == nil {
		t.Fatal("expected error, got nil")
	}

	// Assert MRs and Changes are nil/empty
	if res.MRs != nil && len(res.MRs) > 0 {
		t.Errorf("expected no MRs, got %d", len(res.MRs))
	}
	if res.Changes != nil && len(res.Changes) > 0 {
		t.Errorf("expected no changes, got %d", len(res.Changes))
	}

	// Assert state file was NOT created
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Error("state file should not be written on provider error")
	}
}

func TestFetchSurfacesSaveStateFailure(t *testing.T) {
	// A state path whose parent is a FILE makes MkdirAll/WriteFile fail.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(blocker, "state.json")
	res := Fetch(Deps{Provider: fakeProvider{}, StatePath: statePath, Cfg: config.Default()})
	if res.Err != nil {
		t.Fatalf("save failure must be non-fatal, got Err=%v", res.Err)
	}
	if res.Warning == "" {
		t.Error("expected a Warning when SaveState fails")
	}
}

func TestFetchDropsHiddenChanges(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	Fetch(Deps{Provider: fakeProvider{mrs: []core.MR{mineMR("g/p!1", "success")}},
		Me: "you", StatePath: statePath, Cfg: config.Default()}) // baseline

	res := Fetch(Deps{Provider: fakeProvider{mrs: []core.MR{mineMR("g/p!1", "failed")}},
		Me: "you", StatePath: statePath, Cfg: config.Default(),
		Hidden: map[string]bool{"g/p!1": true}})
	if res.Err != nil {
		t.Fatalf("err: %v", res.Err)
	}
	if len(res.Changes) != 0 {
		t.Errorf("changes on a hidden MR must be muted, got %v", res.Changes)
	}
	if len(res.MRs) != 1 {
		t.Errorf("hidden MRs are still fetched (shown in the Hidden tab), got %d", len(res.MRs))
	}
}
