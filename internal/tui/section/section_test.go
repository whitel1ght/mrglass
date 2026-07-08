package section

import (
	"testing"

	"github.com/whitel1ght/mrglass/internal/core"
)

func TestMatchRole(t *testing.T) {
	mine := core.MR{Role: core.RoleMine}
	if !Match(`role == "mine"`, mine) {
		t.Error("mine should match role==mine")
	}
	if Match(`role == "to_review"`, mine) {
		t.Error("mine should not match role==to_review")
	}
}

func TestMatchBrokenFilterMatchesNothing(t *testing.T) {
	if Match(`this is not valid @#$`, core.MR{}) {
		t.Error("a broken filter should match nothing, not panic/true")
	}
}

func TestMatchSeesRealApprovalsRequired(t *testing.T) {
	mr := core.MR{ApprovalsRequired: 2}
	if !Match(`required == 2`, mr) {
		t.Error("filter should see the MR's real ApprovalsRequired")
	}
	if Match(`required == 0`, mr) {
		t.Error("required must not be hardcoded to 0")
	}
}

func TestFilterUsesPerMRApprovals(t *testing.T) {
	mrs := []core.MR{{Ref: "a", ApprovalsRequired: 2}, {Ref: "b", ApprovalsRequired: 0}}
	got := Filter(`required > 0`, mrs)
	if len(got) != 1 || got[0].Ref != "a" {
		t.Errorf("Filter should evaluate required per MR, got %v", got)
	}
}

func TestGroupByTicketOrdersOtherLast(t *testing.T) {
	mrs := []core.MR{
		{Ref: "g/p!1", TicketKey: "Other"},
		{Ref: "g/p!2", TicketKey: "ABC-9"},
		{Ref: "g/p!3", TicketKey: "ABC-1"},
	}
	keys, groups := GroupByTicket(mrs)
	if len(keys) != 3 || keys[len(keys)-1] != "Other" {
		t.Errorf("Other should sort last, got %v", keys)
	}
	if len(groups["ABC-9"]) != 1 {
		t.Errorf("grouping wrong: %v", groups)
	}
}
