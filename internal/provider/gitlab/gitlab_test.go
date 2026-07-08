package gitlab

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/whitel1ght/mrglass/internal/core"
)

func roleMine() core.Role { return core.RoleMine }

func TestToMRMapsFields(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "mrs.json"))
	if err != nil {
		t.Fatal(err)
	}
	list, err := parseMRList(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 MR, got %d", len(list))
	}
	mr := toMR(list[0], "you", `([A-Z][A-Z0-9]+-\d+)`)
	if mr.Ref != "group/project!177" {
		t.Errorf("Ref = %q", mr.Ref)
	}
	if mr.IID != 177 || mr.ProjectID != 42 {
		t.Errorf("IID/ProjectID = %d/%d", mr.IID, mr.ProjectID)
	}
	if mr.CI != "failed" {
		t.Errorf("CI = %q, want failed", mr.CI)
	}
	if mr.PipelineURL == "" {
		t.Error("PipelineURL should be set")
	}
	if mr.Comments != 2 {
		t.Errorf("Comments = %d", mr.Comments)
	}
	if mr.TicketKey != "ABC-1234" {
		t.Errorf("TicketKey = %q", mr.TicketKey)
	}
	if mr.Role != roleMine() {
		t.Errorf("author==me should be RoleMine, got %v", mr.Role)
	}
}

func TestToMRUnresolvedInversion(t *testing.T) {
	base := rawMR{
		References: struct {
			Full string `json:"full"`
		}{Full: "g/p!1"},
	}

	const pat = `([A-Z][A-Z0-9]+-\d+)`

	// BlockingOK=false → Unresolved=true
	rm := base
	rm.BlockingOK = false
	mr := toMR(rm, "me", pat)
	if !mr.Unresolved {
		t.Errorf("BlockingOK=false: want Unresolved=true, got false")
	}

	// BlockingOK=true → Unresolved=false
	rm2 := base
	rm2.BlockingOK = true
	mr2 := toMR(rm2, "me", pat)
	if mr2.Unresolved {
		t.Errorf("BlockingOK=true: want Unresolved=false, got true")
	}

	// Draft via Draft field
	rm3 := base
	rm3.Draft = true
	rm3.WIP = false
	mr3 := toMR(rm3, "me", pat)
	if !mr3.Draft {
		t.Errorf("Draft=true,WIP=false: want Draft=true, got false")
	}

	// Draft via WIP field
	rm4 := base
	rm4.Draft = false
	rm4.WIP = true
	mr4 := toMR(rm4, "me", pat)
	if !mr4.Draft {
		t.Errorf("Draft=false,WIP=true: want Draft=true, got false")
	}
}

func TestParseApprovals(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "approvals.json"))
	if err != nil {
		t.Fatal(err)
	}
	approvers, required, err := parseApprovals(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(approvers) != 1 || approvers[0] != "alice" {
		t.Errorf("approvers = %v, want [alice]", approvers)
	}
	if required != 2 {
		t.Errorf("required = %d, want 2", required)
	}
}

// fakeAPIRunner returns canned bytes keyed by a substring of the joined args,
// and records every call. Goroutine-safe (List enriches concurrently).
// Named distinctly from glab_test.go's fakeRunner (sequential outs/errs
// slices) to avoid a duplicate-symbol collision within this package.
type fakeAPIRunner struct {
	mu        sync.Mutex
	responses map[string][]byte // substring of strings.Join(args, " ") -> payload
	errFor    string            // args containing this substring return an error
	calls     []string
}

func (f *fakeAPIRunner) Run(args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	joined := strings.Join(args, " ")
	f.calls = append(f.calls, joined)
	if f.errFor != "" && strings.Contains(joined, f.errFor) {
		return nil, errors.New("boom: " + joined)
	}
	for k, v := range f.responses {
		if strings.Contains(joined, k) {
			return v, nil
		}
	}
	return []byte(`[]`), nil
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWhoami(t *testing.T) {
	f := &fakeAPIRunner{responses: map[string][]byte{"api user": []byte(`{"username":"dmitry"}`)}}
	p := &GitLabProvider{R: f}
	me, err := p.Whoami()
	if err != nil || me != "dmitry" {
		t.Fatalf("got %q, %v", me, err)
	}
}

// TestListThreeBucketsDedupeAndEnrich exercises all three List filters plus
// enrich: the created_by_me and assigned_to_me buckets return the same MR
// (fixture has exactly one), so they must dedupe by Ref down to one result,
// and that result must carry a successful approvals enrich.
func TestListThreeBucketsDedupeAndEnrich(t *testing.T) {
	f := &fakeAPIRunner{responses: map[string][]byte{
		"scope=created_by_me":  fixture(t, "mrs.json"),
		"scope=assigned_to_me": fixture(t, "mrs.json"), // same MRs → must dedupe
		"reviewer_username":    []byte(`[]`),
		"/approvals":           fixture(t, "approvals.json"),
	}}
	p := &GitLabProvider{R: f}
	mrs, err := p.List("you", 100000, `([A-Z][A-Z0-9]+-\d+)`)
	if err != nil {
		t.Fatal(err)
	}
	var raw []map[string]any
	if err := json.Unmarshal(fixture(t, "mrs.json"), &raw); err != nil {
		t.Fatal(err)
	}
	if len(mrs) != len(raw) {
		t.Errorf("dedupe failed: got %d MRs, fixture has %d", len(mrs), len(raw))
	}
	for _, mr := range mrs {
		if !mr.ApprovalsOK() {
			t.Errorf("%s: enrich should have succeeded", mr.Ref)
		}
	}
}

func TestListEnrichFailureIsNonFatal(t *testing.T) {
	f := &fakeAPIRunner{
		responses: map[string][]byte{"scope=created_by_me": fixture(t, "mrs.json")},
		errFor:    "/approvals",
	}
	p := &GitLabProvider{R: f}
	mrs, err := p.List("you", 100000, `([A-Z][A-Z0-9]+-\d+)`)
	if err != nil {
		t.Fatal("enrich failure must not fail List")
	}
	for _, mr := range mrs {
		if mr.ApprovalsOK() {
			t.Errorf("%s: ApprovalsOK should be false after enrich failure", mr.Ref)
		}
	}
}

func TestListBucketFetchFailureIsFatal(t *testing.T) {
	f := &fakeAPIRunner{errFor: "scope=created_by_me"}
	p := &GitLabProvider{R: f}
	if _, err := p.List("you", 30, `([A-Z][A-Z0-9]+-\d+)`); err == nil {
		t.Error("bucket fetch failure should fail List")
	}
}

func TestMRDiffConcatenatesChanges(t *testing.T) {
	payload := []byte(`{"changes":[
		{"old_path":"a.go","new_path":"a.go","diff":"@@ -1 +1 @@\n-x\n+y"},
		{"old_path":"gone.go","new_path":"","diff":"@@ deleted @@"}]}`)
	f := &fakeAPIRunner{responses: map[string][]byte{"/changes": payload}}
	p := &GitLabProvider{R: f}
	diff, err := p.MRDiff(core.MR{ProjectID: 7, IID: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "--- a.go\n") || !strings.Contains(diff, "--- gone.go\n") {
		t.Errorf("diff missing file headers (deleted file should use old_path):\n%s", diff)
	}
}

// TestPostNoteArgsExact pins the exact args string built for a note post,
// using the goroutine-safe fakeAPIRunner (glab_test.go's TestPostNoteArgs
// already covers this via substring assertions on the sequential fakeRunner).
func TestPostNoteArgsExact(t *testing.T) {
	f := &fakeAPIRunner{}
	p := &GitLabProvider{R: f}
	if err := p.PostNote(core.MR{ProjectID: 7, IID: 3}, "hello"); err != nil {
		t.Fatal(err)
	}
	want := "api -X POST projects/7/merge_requests/3/notes -f body=hello"
	if len(f.calls) != 1 || f.calls[0] != want {
		t.Errorf("got calls %v, want [%s]", f.calls, want)
	}
}
