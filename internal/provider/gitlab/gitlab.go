package gitlab

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/dmitry/mrglass/internal/core"
	"github.com/dmitry/mrglass/internal/provider"
)

// GitLabProvider implements provider.Provider via the glab CLI.
type GitLabProvider struct {
	R Runner
}

func New() *GitLabProvider { return &GitLabProvider{R: ExecRunner{}} }

type rawMR struct {
	IID          int    `json:"iid"`
	ProjectID    int    `json:"project_id"`
	Title        string `json:"title"`
	WebURL       string `json:"web_url"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	Draft        bool   `json:"draft"`
	WIP          bool   `json:"work_in_progress"`
	HasConflicts bool   `json:"has_conflicts"`
	BlockingOK   bool   `json:"blocking_discussions_resolved"`
	Notes        int    `json:"user_notes_count"`
	MergeStatus  string `json:"detailed_merge_status"`
	UpdatedAt    string `json:"updated_at"`
	Author       struct {
		Username string `json:"username"`
	} `json:"author"`
	Reviewers []struct {
		Username string `json:"username"`
	} `json:"reviewers"`
	References struct {
		Full string `json:"full"`
	} `json:"references"`
	HeadPipeline *struct {
		Status string `json:"status"`
		WebURL string `json:"web_url"`
	} `json:"head_pipeline"`
	Pipeline *struct {
		Status string `json:"status"`
		WebURL string `json:"web_url"`
	} `json:"pipeline"`
}

func (p *GitLabProvider) Whoami() (string, error) {
	out, err := APIGet(p.R, "user", 2)
	if err != nil {
		return "", err
	}
	var u struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(out, &u); err != nil {
		return "", err
	}
	return u.Username, nil
}

func (p *GitLabProvider) List(me string, days int, ticketPattern string) ([]core.MR, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).
		Format("2006-01-02T15:04:05Z")
	filters := []string{
		"scope=created_by_me",
		"scope=assigned_to_me",
		"scope=all&reviewer_username=" + url.QueryEscape(me),
	}
	found := map[string]core.MR{}
	for _, flt := range filters {
		path := fmt.Sprintf("merge_requests?%s&state=opened&updated_after=%s&per_page=100",
			flt, cutoff)
		out, err := APIGet(p.R, path, 2)
		if err != nil {
			return nil, err
		}
		list, err := parseMRList(out)
		if err != nil {
			return nil, err
		}
		for _, rm := range list {
			mr := toMR(rm, me, ticketPattern)
			if _, seen := found[mr.Ref]; !seen {
				found[mr.Ref] = mr
			}
		}
	}
	result := make([]core.MR, 0, len(found))
	for _, mr := range found {
		mr = p.enrich(mr)
		result = append(result, mr)
	}
	return result, nil
}

func (p *GitLabProvider) enrich(mr core.MR) core.MR {
	path := fmt.Sprintf("projects/%d/merge_requests/%d/approvals", mr.ProjectID, mr.IID)
	out, err := APIGet(p.R, path, 2)
	if err != nil {
		mr.SetApprovalsOK(false)
		return mr
	}
	approvers, err := parseApprovers(out)
	if err != nil {
		mr.SetApprovalsOK(false)
		return mr
	}
	mr.ApprovedBy = approvers
	mr.SetApprovalsOK(true)
	return mr
}

func parseMRList(raw []byte) ([]rawMR, error) {
	var list []rawMR
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func parseApprovers(raw []byte) ([]string, error) {
	var a struct {
		ApprovedBy []struct {
			User struct {
				Username string `json:"username"`
			} `json:"user"`
		} `json:"approved_by"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(a.ApprovedBy))
	for _, x := range a.ApprovedBy {
		out = append(out, x.User.Username)
	}
	return out, nil
}

func toMR(rm rawMR, me, ticketPattern string) core.MR {
	head := rm.HeadPipeline
	if head == nil {
		head = rm.Pipeline
	}
	ci, pipeURL := "", ""
	if head != nil {
		ci, pipeURL = head.Status, head.WebURL
	}
	var reviewers []string
	amReviewer := false
	for _, r := range rm.Reviewers {
		reviewers = append(reviewers, r.Username)
		if r.Username == me {
			amReviewer = true
		}
	}
	role := core.RoleToReview
	switch {
	case rm.Author.Username == me:
		role = core.RoleMine
	case amReviewer:
		role = core.RoleReviewRequested
	}
	updated, _ := time.Parse(time.RFC3339, rm.UpdatedAt)
	return core.MR{
		Ref:          rm.References.Full,
		IID:          rm.IID,
		ProjectID:    rm.ProjectID,
		Title:        rm.Title,
		URL:          rm.WebURL,
		Author:       rm.Author.Username,
		SourceBranch: rm.SourceBranch,
		TargetBranch: rm.TargetBranch,
		Role:         role,
		Reviewers:    reviewers,
		CI:           ci,
		PipelineURL:  pipeURL,
		Conflicts:    rm.HasConflicts,
		Unresolved:   !rm.BlockingOK,
		Comments:     rm.Notes,
		Draft:        rm.Draft || rm.WIP,
		MergeStatus:  rm.MergeStatus,
		UpdatedAt:    updated,
		TicketKey:    core.ParseTicket(rm.Title, rm.SourceBranch, ticketPattern),
	}
}

var _ = core.MR{} // ensure core import used even if signatures change

// compile-time check
var _ provider.Provider = (*GitLabProvider)(nil)
