package section

import (
	"testing"

	"github.com/whitel1ght/mrglass/internal/core"
)

func TestMatchRole(t *testing.T) {
	mine := core.MR{Role: core.RoleMine}
	if !Match(`role == "mine"`, mine, 0) {
		t.Error("mine should match role==mine")
	}
	if Match(`role == "to_review"`, mine, 0) {
		t.Error("mine should not match role==to_review")
	}
}

func TestMatchBrokenFilterMatchesNothing(t *testing.T) {
	if Match(`this is not valid @#$`, core.MR{}, 0) {
		t.Error("a broken filter should match nothing, not panic/true")
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
