package core

import (
	"strings"
	"testing"
)

func snap(ref, ci string, approvers []string) Snapshot {
	return Snapshot{Ref: ref, URL: "u", Title: "t", CI: ci, ApprovedBy: approvers}
}

func byRef(cs []Change, ref string) *Change {
	for i := range cs {
		if cs[i].Ref == ref {
			return &cs[i]
		}
	}
	return nil
}

func TestDiffNew(t *testing.T) {
	curr := map[string]Snapshot{"g/p!1": snap("g/p!1", "success", nil)}
	cs := Diff(map[string]Snapshot{}, curr)
	c := byRef(cs, "g/p!1")
	if c == nil || c.Kind != KindNew {
		t.Fatalf("want a KindNew change for g/p!1, got %+v", cs)
	}
}

func TestDiffGone(t *testing.T) {
	prev := map[string]Snapshot{"g/p!1": snap("g/p!1", "success", nil)}
	cs := Diff(prev, map[string]Snapshot{})
	c := byRef(cs, "g/p!1")
	if c == nil || c.Kind != KindGone {
		t.Fatalf("want a KindGone change, got %+v", cs)
	}
}

func TestDiffCIChange(t *testing.T) {
	prev := map[string]Snapshot{"g/p!1": snap("g/p!1", "success", nil)}
	curr := map[string]Snapshot{"g/p!1": snap("g/p!1", "failed", nil)}
	cs := Diff(prev, curr)
	c := byRef(cs, "g/p!1")
	if c == nil || c.Kind != KindChanged {
		t.Fatalf("want a KindChanged change, got %+v", cs)
	}
	found := false
	for _, f := range c.Fields {
		if f.Field == "ci" && f.Old == "success" && f.New == "failed" {
			found = true
		}
	}
	if !found {
		t.Errorf("want a ci field change success→failed, got %+v", c.Fields)
	}
	if !strings.Contains(c.Detail, "CI") {
		t.Errorf("Detail %q should mention CI", c.Detail)
	}
}

func TestDiffApprovalGainedAndLost(t *testing.T) {
	prev := map[string]Snapshot{"g/p!1": snap("g/p!1", "success", []string{"bob"})}
	curr := map[string]Snapshot{"g/p!1": snap("g/p!1", "success", []string{"alice"})}
	c := byRef(Diff(prev, curr), "g/p!1")
	if c == nil {
		t.Fatal("want a change")
	}
	if !strings.Contains(c.Detail, "alice") || !strings.Contains(c.Detail, "bob") {
		t.Errorf("Detail %q should mention gained alice and lost bob", c.Detail)
	}
}

func TestDiffCommentsOnlyIncrease(t *testing.T) {
	prev := map[string]Snapshot{"g/p!1": {Ref: "g/p!1", Comments: 5}}
	currUp := map[string]Snapshot{"g/p!1": {Ref: "g/p!1", Comments: 7}}
	if c := byRef(Diff(prev, currUp), "g/p!1"); c == nil {
		t.Error("want a change when comments increase")
	}
	currDown := map[string]Snapshot{"g/p!1": {Ref: "g/p!1", Comments: 3}}
	if c := byRef(Diff(prev, currDown), "g/p!1"); c != nil {
		t.Error("comment count decrease should not be a change")
	}
}

func TestDiffNoChangeIsEmpty(t *testing.T) {
	s := map[string]Snapshot{"g/p!1": snap("g/p!1", "success", []string{"alice"})}
	if cs := Diff(s, s); len(cs) != 0 {
		t.Errorf("identical snapshots should yield no changes, got %+v", cs)
	}
}
