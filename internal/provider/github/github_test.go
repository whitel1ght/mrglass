package github

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whitel1ght/mrglass/internal/core"
	"github.com/whitel1ght/mrglass/internal/provider/execx"
)

const pat = `([A-Z][A-Z0-9]+-\d+)`

// fakeRunner returns canned bytes per gh subcommand and records the args of the
// search queries so we can assert the 3-bucket flags. No network.
type fakeRunner struct {
	search  []byte
	prview  []byte
	user    []byte
	calls   [][]string
	failPRV bool
}

func (f *fakeRunner) Run(args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	switch {
	case len(args) >= 1 && args[0] == "api":
		return f.user, nil
	case len(args) >= 2 && args[0] == "search" && args[1] == "prs":
		return f.search, nil
	case len(args) >= 2 && args[0] == "pr" && args[1] == "view":
		if f.failPRV {
			return nil, os.ErrPermission
		}
		return f.prview, nil
	}
	return nil, nil
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func jsonUnmarshalFixture(t *testing.T, name string, v any) error {
	t.Helper()
	return json.Unmarshal(loadFixture(t, name), v)
}

func TestToMRMapsSearchFields(t *testing.T) {
	list, err := parseSearch(loadFixture(t, "search.json"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 search results, got %d", len(list))
	}
	mr := toMR(list[0], "you", core.RoleToReview, pat)
	if mr.Ref != "owner/repo#123" {
		t.Errorf("Ref = %q, want owner/repo#123", mr.Ref)
	}
	if mr.IID != 123 {
		t.Errorf("IID = %d, want 123", mr.IID)
	}
	if mr.Author != "you" {
		t.Errorf("Author = %q", mr.Author)
	}
	// author==me must win over the bucket role.
	if mr.Role != core.RoleMine {
		t.Errorf("Role = %v, want RoleMine (author==me)", mr.Role)
	}
	if mr.Comments != 4 {
		t.Errorf("Comments = %d, want 4", mr.Comments)
	}
	if mr.TicketKey != "ABC-1234" {
		t.Errorf("TicketKey = %q, want ABC-1234", mr.TicketKey)
	}
}

func TestToMRBucketRole(t *testing.T) {
	list, _ := parseSearch(loadFixture(t, "search.json"))
	// list[1] is authored by carol, not "you": bucket role should stick.
	mr := toMR(list[1], "you", core.RoleReviewRequested, pat)
	if mr.Role != core.RoleReviewRequested {
		t.Errorf("Role = %v, want RoleReviewRequested", mr.Role)
	}
}

func TestApplyEnrich(t *testing.T) {
	var pv prView
	if err := jsonUnmarshalFixture(t, "prview.json", &pv); err != nil {
		t.Fatal(err)
	}
	base := core.MR{Ref: "owner/repo#123", Title: "Fix ABC-1234 the widget crash", Role: core.RoleMine}
	mr := applyEnrich(base, pv, "you", pat)

	if mr.SourceBranch != "feature/ABC-1234-widget" {
		t.Errorf("SourceBranch = %q", mr.SourceBranch)
	}
	if mr.TargetBranch != "main" {
		t.Errorf("TargetBranch = %q", mr.TargetBranch)
	}
	if len(mr.ApprovedBy) != 1 || mr.ApprovedBy[0] != "alice" {
		t.Errorf("ApprovedBy = %v, want [alice]", mr.ApprovedBy)
	}
	if !mr.ApprovalsOK() {
		t.Error("ApprovalsOK should be true after successful enrich")
	}
	if mr.ApprovalsRequired != 0 {
		t.Errorf("ApprovalsRequired = %d, want 0", mr.ApprovalsRequired)
	}
	if mr.CI != "failed" {
		t.Errorf("CI = %q, want failed (a FAILURE present)", mr.CI)
	}
	if !mr.Conflicts {
		t.Error("Conflicts should be true for mergeable=CONFLICTING")
	}
	if mr.MergeStatus != "DIRTY" {
		t.Errorf("MergeStatus = %q, want DIRTY", mr.MergeStatus)
	}
	if mr.Unresolved {
		t.Error("Unresolved should be false (not derivable from this field set)")
	}
	// reviewRequests includes "you"; it must be excluded.
	if len(mr.Reviewers) != 1 || mr.Reviewers[0] != "carol" {
		t.Errorf("Reviewers = %v, want [carol]", mr.Reviewers)
	}
	if mr.TicketKey != "ABC-1234" {
		t.Errorf("TicketKey = %q, want ABC-1234", mr.TicketKey)
	}
}

func TestCIFromRollup(t *testing.T) {
	cases := []struct {
		name   string
		rollup []rollupCheck
		want   string
	}{
		{"empty", nil, ""},
		{"failure dominates", []rollupCheck{{Conclusion: "SUCCESS"}, {Conclusion: "FAILURE"}}, "failed"},
		{"running", []rollupCheck{{Status: "IN_PROGRESS"}, {Conclusion: "SUCCESS"}}, "running"},
		{"all success", []rollupCheck{{Conclusion: "SUCCESS"}, {Conclusion: "SKIPPED"}}, "success"},
		{"status context pending", []rollupCheck{{State: "PENDING"}}, "running"},
		{"status context error is failed", []rollupCheck{{State: "ERROR"}}, "failed"},
		{"cancelled is failed", []rollupCheck{{Conclusion: "CANCELLED"}}, "failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ciFromRollup(c.rollup); got != c.want {
				t.Errorf("ciFromRollup = %q, want %q", got, c.want)
			}
		})
	}
}

func TestListThreeBucketsAndDedupe(t *testing.T) {
	f := &fakeRunner{
		search: loadFixture(t, "search.json"),
		prview: loadFixture(t, "prview.json"),
	}
	p := &GitHubProvider{R: f}
	// days large enough to keep the fixtures regardless of "now".
	mrs, err := p.List("you", 100000, pat)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// search.json has 2 PRs; both repeated across 3 buckets -> deduped to 2.
	if len(mrs) != 2 {
		t.Fatalf("want 2 deduped MRs, got %d", len(mrs))
	}

	// Assert the 3 bucket queries were issued with the right flags.
	wantFlags := map[string]bool{
		"--author=@me":           false,
		"--assignee=@me":         false,
		"--review-requested=@me": false,
	}
	for _, call := range f.calls {
		if len(call) >= 2 && call[0] == "search" && call[1] == "prs" {
			for _, a := range call {
				if _, ok := wantFlags[a]; ok {
					wantFlags[a] = true
				}
			}
		}
	}
	for flag, seen := range wantFlags {
		if !seen {
			t.Errorf("search bucket flag %q was never issued", flag)
		}
	}
}

func TestEnrichCarriesApprovalsFailure(t *testing.T) {
	f := &fakeRunner{failPRV: true}
	p := &GitHubProvider{R: f}
	mr := p.enrich(core.MR{Ref: "owner/repo#7"}, "you", pat)
	if mr.ApprovalsOK() {
		t.Error("enrich failure should leave ApprovalsOK false")
	}
}

func TestWhoami(t *testing.T) {
	f := &fakeRunner{user: []byte("octocat\n")}
	p := &GitHubProvider{R: f}
	who, err := p.Whoami()
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if who != "octocat" {
		t.Errorf("Whoami = %q, want octocat", who)
	}
}

// flakyRunner fails its first N calls with a transient error, then delegates
// to inner. Used to prove reads go through execx.Retry.
type flakyRunner struct {
	fails int // fail this many calls with a transient error, then succeed
	inner Runner
	calls int
}

func (f *flakyRunner) Run(args ...string) ([]byte, error) {
	f.calls++
	if f.calls <= f.fails {
		return nil, errors.New("unexpected EOF")
	}
	return f.inner.Run(args...)
}

func TestWhoamiRetriesTransient(t *testing.T) {
	execx.Sleep = func(time.Duration) {}
	defer func() { execx.Sleep = time.Sleep }()
	inner := &fakeRunner{user: []byte("octocat\n")}
	p := &GitHubProvider{R: &flakyRunner{fails: 1, inner: inner}}
	me, err := p.Whoami()
	if err != nil {
		t.Fatalf("want retry success, got %v", err)
	}
	if me == "" {
		t.Error("want a username after retry")
	}
}

func TestMRDiffAndPostNoteArgs(t *testing.T) {
	f := &fakeRunner{}
	p := &GitHubProvider{R: f}
	prMR := core.MR{Ref: "owner/repo#123"}
	if _, err := p.MRDiff(prMR); err != nil {
		t.Fatalf("MRDiff: %v", err)
	}
	if err := p.PostNote(prMR, "hi"); err != nil {
		t.Fatalf("PostNote: %v", err)
	}
	var sawDiff, sawComment bool
	for _, call := range f.calls {
		j := strings.Join(call, " ")
		if strings.HasPrefix(j, "pr diff 123 --repo owner/repo") {
			sawDiff = true
		}
		if strings.HasPrefix(j, "pr comment 123 --repo owner/repo --body hi") {
			sawComment = true
		}
	}
	if !sawDiff {
		t.Error("MRDiff did not invoke `gh pr diff 123 --repo owner/repo`")
	}
	if !sawComment {
		t.Error("PostNote did not invoke `gh pr comment 123 --repo owner/repo --body hi`")
	}
}
