package core

import (
	"reflect"
	"testing"
)

func TestSnapSortsApprovers(t *testing.T) {
	m := MR{Ref: "g/p!1", ApprovedBy: []string{"bob", "alice"}}
	m.SetApprovalsOK(true)
	got := Snap(m, nil)
	if !reflect.DeepEqual(got.ApprovedBy, []string{"alice", "bob"}) {
		t.Errorf("ApprovedBy = %v, want sorted [alice bob]", got.ApprovedBy)
	}
}

func TestSnapCarriesApproversWhenFetchFailed(t *testing.T) {
	prev := &Snapshot{ApprovedBy: []string{"alice"}}
	m := MR{Ref: "g/p!1", ApprovedBy: nil} // fetch failed → empty
	m.SetApprovalsOK(false)
	got := Snap(m, prev)
	if !reflect.DeepEqual(got.ApprovedBy, []string{"alice"}) {
		t.Errorf("ApprovedBy = %v, want carried-forward [alice]", got.ApprovedBy)
	}
}

func TestSnapReducesFields(t *testing.T) {
	m := MR{
		Ref: "g/p!1", Title: "t", URL: "u", CI: "failed",
		Conflicts: true, Unresolved: true, Comments: 3, Draft: true,
		MergeStatus: "conflict",
	}
	m.SetApprovalsOK(true)
	got := Snap(m, nil)
	want := Snapshot{
		Ref: "g/p!1", Title: "t", URL: "u", CI: "failed",
		ApprovedBy: []string{}, Conflicts: true, Unresolved: true,
		Comments: 3, Draft: true, MergeStatus: "conflict",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Snap = %+v, want %+v", got, want)
	}
}
