package core

import "sort"

// Snapshot is the reduced, diff-relevant view of an MR. Cosmetic fields
// (updated_at, etc.) are deliberately excluded.
type Snapshot struct {
	Ref         string   `json:"ref"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	CI          string   `json:"ci"`
	ApprovedBy  []string `json:"approved_by"`
	Conflicts   bool     `json:"conflicts"`
	Unresolved  bool     `json:"unresolved"`
	Comments    int      `json:"comments"`
	Draft       bool     `json:"draft"`
	MergeStatus string   `json:"merge_status"`
}

// Snap reduces an MR to a Snapshot. When the approvals fetch failed this run
// (m.ApprovalsOK()==false), the prior snapshot's approvers are carried forward
// so a transient failure is never mistaken for "all approvals removed".
func Snap(m MR, prev *Snapshot) Snapshot {
	var approvers []string
	if m.ApprovalsOK() {
		approvers = append([]string{}, m.ApprovedBy...)
		sort.Strings(approvers)
	} else if prev != nil {
		approvers = append([]string{}, prev.ApprovedBy...)
	} else {
		approvers = []string{}
	}
	return Snapshot{
		Ref: m.Ref, Title: m.Title, URL: m.URL, CI: m.CI,
		ApprovedBy: approvers, Conflicts: m.Conflicts, Unresolved: m.Unresolved,
		Comments: m.Comments, Draft: m.Draft, MergeStatus: m.MergeStatus,
	}
}
