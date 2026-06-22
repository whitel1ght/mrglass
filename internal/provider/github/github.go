package github

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dmitry/mrglass/internal/core"
	"github.com/dmitry/mrglass/internal/provider"
)

// GitHubProvider implements provider.Provider via the gh CLI. It mirrors the
// GitLab provider: a 3-bucket search (authored/assigned/review-requested),
// dedupe by Ref with first-bucket-wins precedence, then a per-PR enrich call.
type GitHubProvider struct {
	R Runner
}

func New() *GitHubProvider { return &GitHubProvider{R: ExecRunner{}} }

// searchFields is the common --json field set requested for every search bucket.
const searchFields = "number,title,url,repository,author,isDraft,createdAt,updatedAt,commentsCount"

// searchResult is one row from `gh search prs --json ...`.
type searchResult struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	IsDraft       bool   `json:"isDraft"`
	CommentsCount int    `json:"commentsCount"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

// prView is the shape returned by `gh pr view --json ...` for enrichment.
type prView struct {
	Number           int    `json:"number"`
	HeadRefName      string `json:"headRefName"`
	BaseRefName      string `json:"baseRefName"`
	IsDraft          bool   `json:"isDraft"`
	Mergeable        string `json:"mergeable"`
	MergeStateStatus string `json:"mergeStateStatus"`
	ReviewDecision   string `json:"reviewDecision"`
	LatestReviews    []struct {
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		State string `json:"state"`
	} `json:"latestReviews"`
	StatusCheckRollup []rollupCheck `json:"statusCheckRollup"`
	Assignees         []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	ReviewRequests []struct {
		Login string `json:"login"`
	} `json:"reviewRequests"`
}

// rollupCheck is one entry of statusCheckRollup. GitHub mixes two check shapes:
// CheckRun (has `status` + `conclusion`) and StatusContext (has `state`). We
// read all three so ciFromRollup can classify either kind.
type rollupCheck struct {
	Status     string `json:"status"`     // CheckRun: QUEUED|IN_PROGRESS|COMPLETED
	Conclusion string `json:"conclusion"` // CheckRun: SUCCESS|FAILURE|...
	State      string `json:"state"`      // StatusContext: SUCCESS|FAILURE|PENDING|ERROR
}

func (p *GitHubProvider) Whoami() (string, error) {
	out, err := run(p.R, "api", "user", "--jq", ".login")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (p *GitHubProvider) List(me string, days int, ticketPattern string) ([]core.MR, error) {
	// First bucket to set a Ref wins, giving Role precedence:
	// author > assigned > review-requested.
	buckets := []struct {
		flag string
		role core.Role
	}{
		{"--author=@me", core.RoleMine},
		{"--assignee=@me", core.RoleToReview},
		{"--review-requested=@me", core.RoleReviewRequested},
	}
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)

	found := map[string]core.MR{}
	for _, b := range buckets {
		args := []string{
			"search", "prs", b.flag, "--state=open",
			"--limit", "100", "--json", searchFields,
		}
		out, err := run(p.R, args...)
		if err != nil {
			return nil, err
		}
		list, err := parseSearch(out)
		if err != nil {
			return nil, err
		}
		for _, sr := range list {
			mr := toMR(sr, me, b.role, ticketPattern)
			// Skip PRs not touched within the window. A zero/unparseable
			// timestamp keeps the PR rather than silently dropping it.
			if !mr.UpdatedAt.IsZero() && mr.UpdatedAt.Before(cutoff) {
				continue
			}
			if _, seen := found[mr.Ref]; !seen {
				found[mr.Ref] = mr
			}
		}
	}

	result := make([]core.MR, 0, len(found))
	for _, mr := range found {
		mr = p.enrich(mr, me, ticketPattern)
		result = append(result, mr)
	}
	return result, nil
}

// enrich fetches per-PR detail (branches, approvals, CI, conflicts, reviewers)
// that the search API doesn't return. On any fetch/parse failure it mirrors the
// GitLab provider: mark approvals as not-OK and return the partial MR unchanged
// so the row still renders.
func (p *GitHubProvider) enrich(mr core.MR, me, ticketPattern string) core.MR {
	repo, num, ok := splitRef(mr.Ref)
	if !ok {
		mr.SetApprovalsOK(false)
		return mr
	}
	out, err := run(p.R, "pr", "view", strconv.Itoa(num), "--repo", repo, "--json",
		"number,headRefName,baseRefName,isDraft,mergeable,mergeStateStatus,reviewDecision,latestReviews,statusCheckRollup,assignees,reviewRequests")
	if err != nil {
		mr.SetApprovalsOK(false)
		return mr
	}
	var pv prView
	if err := json.Unmarshal(out, &pv); err != nil {
		mr.SetApprovalsOK(false)
		return mr
	}
	return applyEnrich(mr, pv, me, ticketPattern)
}

// MRDiff returns the unified diff for a PR via `gh pr diff`.
//
// MRDiff returns the PR's diff via `gh pr diff`. Implements review.ReviewForge —
// it takes the whole core.MR and parses owner/repo#number from the Ref.
func (p *GitHubProvider) MRDiff(mr core.MR) (string, error) {
	repo, number, ok := splitRef(mr.Ref)
	if !ok {
		return "", fmt.Errorf("github: cannot parse repo/number from ref %q", mr.Ref)
	}
	out, err := run(p.R, "pr", "diff", strconv.Itoa(number), "--repo", repo)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// PostNote posts a comment on a PR via `gh pr comment`. This is a write; the
// caller gates it behind user confirmation. Implements review.ReviewForge.
func (p *GitHubProvider) PostNote(mr core.MR, body string) error {
	repo, number, ok := splitRef(mr.Ref)
	if !ok {
		return fmt.Errorf("github: cannot parse repo/number from ref %q", mr.Ref)
	}
	_, err := run(p.R, "pr", "comment", strconv.Itoa(number), "--repo", repo, "--body", body)
	return err
}

// --- pure mapping helpers (unit-tested against fixtures, no gh calls) ---

func parseSearch(raw []byte) ([]searchResult, error) {
	var list []searchResult
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// toMR maps a search row to a partial core.MR. Role is the bucket's role unless
// the PR is authored by me (RoleMine always wins). Ref is "owner/repo#number".
func toMR(sr searchResult, me string, bucketRole core.Role, ticketPattern string) core.MR {
	role := bucketRole
	if sr.Author.Login == me {
		role = core.RoleMine
	}
	updated, _ := time.Parse(time.RFC3339, sr.UpdatedAt)
	return core.MR{
		Ref:       fmt.Sprintf("%s#%d", sr.Repository.NameWithOwner, sr.Number),
		IID:       sr.Number,
		Title:     sr.Title,
		URL:       sr.URL,
		Author:    sr.Author.Login,
		Role:      role,
		Comments:  sr.CommentsCount,
		Draft:     sr.IsDraft,
		UpdatedAt: updated,
		TicketKey: core.ParseTicket(sr.Title, "", ticketPattern),
	}
}

// applyEnrich folds prView detail into a partial MR.
func applyEnrich(mr core.MR, pv prView, me, ticketPattern string) core.MR {
	mr.SourceBranch = pv.HeadRefName
	mr.TargetBranch = pv.BaseRefName
	mr.Draft = pv.IsDraft
	mr.Conflicts = pv.Mergeable == "CONFLICTING"
	mr.MergeStatus = pv.MergeStateStatus
	mr.CI = ciFromRollup(pv.StatusCheckRollup)
	// GitHub exposes no per-PR required-approval count via this API; 0 means
	// core.Approved treats any genuine approval as approved (matching GitLab
	// when required==0).
	mr.ApprovalsRequired = 0
	mr.ApprovedBy = approversFromReviews(pv.LatestReviews)
	mr.SetApprovalsOK(true)
	// TODO: GitHub unresolved-review-thread state isn't in this field set
	// (needs the GraphQL reviewThreads connection); leave Unresolved=false.
	mr.Unresolved = false

	reviewers := make([]string, 0, len(pv.ReviewRequests))
	for _, r := range pv.ReviewRequests {
		if r.Login == me {
			continue
		}
		reviewers = append(reviewers, r.Login)
	}
	mr.Reviewers = reviewers

	// Re-parse the ticket now that we know the head branch.
	mr.TicketKey = core.ParseTicket(mr.Title, pv.HeadRefName, ticketPattern)
	return mr
}

// approversFromReviews extracts the logins of reviewers whose latest review is
// APPROVED.
func approversFromReviews(reviews []struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	State string `json:"state"`
}) []string {
	out := make([]string, 0, len(reviews))
	for _, r := range reviews {
		if r.State == "APPROVED" {
			out = append(out, r.Author.Login)
		}
	}
	return out
}

// ciFromRollup collapses GitHub's per-check rollup into mrglass's CI vocabulary
// ("success"|"failed"|"running"|""). Failure dominates, then in-progress, then
// success; an empty rollup means "no CI" ("").
func ciFromRollup(rollup []rollupCheck) string {
	if len(rollup) == 0 {
		return ""
	}
	anyRunning := false
	for _, c := range rollup {
		switch strings.ToUpper(c.Conclusion) {
		case "FAILURE", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
			return "failed"
		}
		// StatusContext kind reports via .state instead of .conclusion.
		switch strings.ToUpper(c.State) {
		case "FAILURE", "ERROR":
			return "failed"
		case "PENDING":
			anyRunning = true
		}
		switch strings.ToUpper(c.Status) {
		case "QUEUED", "IN_PROGRESS", "PENDING", "WAITING", "REQUESTED":
			anyRunning = true
		}
	}
	if anyRunning {
		return "running"
	}
	return "success"
}

// splitRef parses "owner/repo#number" back into its repo and PR number.
func splitRef(ref string) (repo string, number int, ok bool) {
	i := strings.LastIndex(ref, "#")
	if i < 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(ref[i+1:])
	if err != nil {
		return "", 0, false
	}
	return ref[:i], n, true
}

var _ = core.MR{} // ensure core import used even if signatures change

// compile-time check
var _ provider.Provider = (*GitHubProvider)(nil)
