package core

import (
	"regexp"
	"strings"
	"time"
)

type Role int

const (
	RoleMine Role = iota
	RoleReviewRequested
	RoleToReview
)

func (r Role) String() string {
	switch r {
	case RoleMine:
		return "mine"
	case RoleReviewRequested:
		return "review_requested"
	case RoleToReview:
		return "to_review"
	default:
		return "unknown"
	}
}

// MR is the unified, provider-agnostic merge-request model.
type MR struct {
	Ref          string
	IID          int
	ProjectID    int
	Title        string
	URL          string
	Author       string
	SourceBranch string
	TargetBranch string

	Role      Role
	Reviewers []string

	CI          string
	PipelineURL string
	ApprovedBy         []string
	ApprovalsRequired  int      // number of approvals GitLab requires (0 = none required)
	Conflicts          bool
	Unresolved  bool
	Comments    int
	Draft       bool
	MergeStatus string

	UpdatedAt time.Time
	TicketKey string

	approvalsOK bool
}

// SetApprovalsOK records whether the approvals fetch succeeded this run.
func (m *MR) SetApprovalsOK(ok bool) { m.approvalsOK = ok }

// ApprovalsOK reports whether the approvals fetch succeeded this run.
func (m *MR) ApprovalsOK() bool { return m.approvalsOK }

// Approved reports whether the MR is approved. When a non-zero approval
// requirement is known, that many genuine approvals must be present; when no
// requirement is set (required==0, e.g. a project with no approval rule or the
// value unavailable), any genuine approval counts. GitLab's misleading
// approved=true flag is never consulted — only the real approver list.
func Approved(approvedBy []string, required int) bool {
	if required > 0 {
		return len(approvedBy) >= required
	}
	return len(approvedBy) > 0
}

// ParseTicket extracts a ticket key from the title, then the branch, upper-cased.
// Returns "Other" when neither matches.
func ParseTicket(title, branch, pattern string) string {
	re := regexp.MustCompile(pattern)
	for _, s := range []string{title, branch} {
		if m := re.FindStringSubmatch(s); m != nil {
			return strings.ToUpper(m[1])
		}
	}
	return "Other"
}
