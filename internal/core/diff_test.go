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

func TestDiffConflictsAppearAndClear(t *testing.T) {
	// Test: Conflicts false → true gives "conflicts appeared"
	prev := map[string]Snapshot{"g/p!1": {Ref: "g/p!1", URL: "u", Title: "t", Conflicts: false}}
	curr := map[string]Snapshot{"g/p!1": {Ref: "g/p!1", URL: "u", Title: "t", Conflicts: true}}
	c := byRef(Diff(prev, curr), "g/p!1")
	if c == nil || c.Kind != KindChanged {
		t.Fatalf("want a KindChanged change, got %+v", c)
	}
	found := false
	for _, f := range c.Fields {
		if f.Field == "conflicts" && f.Old == false && f.New == true {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("want a conflicts field change false→true, got %+v", c.Fields)
	}
	if !strings.Contains(c.Detail, "conflicts appeared") {
		t.Errorf("Detail %q should contain 'conflicts appeared', got %q", c.Detail, c.Detail)
	}

	// Test: Conflicts true → false gives "conflicts resolved"
	prev2 := map[string]Snapshot{"g/p!1": {Ref: "g/p!1", URL: "u", Title: "t", Conflicts: true}}
	curr2 := map[string]Snapshot{"g/p!1": {Ref: "g/p!1", URL: "u", Title: "t", Conflicts: false}}
	c2 := byRef(Diff(prev2, curr2), "g/p!1")
	if c2 == nil || c2.Kind != KindChanged {
		t.Fatalf("want a KindChanged change, got %+v", c2)
	}
	if !strings.Contains(c2.Detail, "conflicts resolved") {
		t.Errorf("Detail %q should contain 'conflicts resolved', got %q", c2.Detail, c2.Detail)
	}
}

func TestDiffUnresolvedAppearAndClear(t *testing.T) {
	// Test: Unresolved false → true gives "new unresolved threads"
	prev := map[string]Snapshot{"g/p!1": {Ref: "g/p!1", URL: "u", Title: "t", Unresolved: false}}
	curr := map[string]Snapshot{"g/p!1": {Ref: "g/p!1", URL: "u", Title: "t", Unresolved: true}}
	c := byRef(Diff(prev, curr), "g/p!1")
	if c == nil || c.Kind != KindChanged {
		t.Fatalf("want a KindChanged change, got %+v", c)
	}
	found := false
	for _, f := range c.Fields {
		if f.Field == "unresolved" && f.Old == false && f.New == true {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("want an unresolved field change false→true, got %+v", c.Fields)
	}
	if !strings.Contains(c.Detail, "new unresolved threads") {
		t.Errorf("Detail %q should contain 'new unresolved threads', got %q", c.Detail, c.Detail)
	}

	// Test: Unresolved true → false gives "threads resolved"
	prev2 := map[string]Snapshot{"g/p!1": {Ref: "g/p!1", URL: "u", Title: "t", Unresolved: true}}
	curr2 := map[string]Snapshot{"g/p!1": {Ref: "g/p!1", URL: "u", Title: "t", Unresolved: false}}
	c2 := byRef(Diff(prev2, curr2), "g/p!1")
	if c2 == nil || c2.Kind != KindChanged {
		t.Fatalf("want a KindChanged change, got %+v", c2)
	}
	if !strings.Contains(c2.Detail, "threads resolved") {
		t.Errorf("Detail %q should contain 'threads resolved', got %q", c2.Detail, c2.Detail)
	}
}

func TestDiffDraftToggle(t *testing.T) {
	// Test: Draft false → true gives "marked draft"
	prev := map[string]Snapshot{"g/p!1": {Ref: "g/p!1", URL: "u", Title: "t", Draft: false}}
	curr := map[string]Snapshot{"g/p!1": {Ref: "g/p!1", URL: "u", Title: "t", Draft: true}}
	c := byRef(Diff(prev, curr), "g/p!1")
	if c == nil || c.Kind != KindChanged {
		t.Fatalf("want a KindChanged change, got %+v", c)
	}
	found := false
	for _, f := range c.Fields {
		if f.Field == "draft" && f.Old == false && f.New == true {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("want a draft field change false→true, got %+v", c.Fields)
	}
	if !strings.Contains(c.Detail, "marked draft") {
		t.Errorf("Detail %q should contain 'marked draft', got %q", c.Detail, c.Detail)
	}

	// Test: Draft true → false gives "marked ready"
	prev2 := map[string]Snapshot{"g/p!1": {Ref: "g/p!1", URL: "u", Title: "t", Draft: true}}
	curr2 := map[string]Snapshot{"g/p!1": {Ref: "g/p!1", URL: "u", Title: "t", Draft: false}}
	c2 := byRef(Diff(prev2, curr2), "g/p!1")
	if c2 == nil || c2.Kind != KindChanged {
		t.Fatalf("want a KindChanged change, got %+v", c2)
	}
	if !strings.Contains(c2.Detail, "marked ready") {
		t.Errorf("Detail %q should contain 'marked ready', got %q", c2.Detail, c2.Detail)
	}
}
