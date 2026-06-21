package analyze

import (
	"testing"

	"github.com/dmitry/mrglass/internal/core"
)

func changed(fields ...core.FieldChange) core.Change {
	return core.Change{Ref: "g/p!1", Kind: core.KindChanged, Fields: fields}
}

func TestTriageWorthyCIFailed(t *testing.T) {
	c := changed(core.FieldChange{Field: "ci", Old: "running", New: "failed"})
	if !IsTriageWorthy(c) {
		t.Error("CI→failed should be triage-worthy")
	}
}

func TestTriageNotWorthyCIPassed(t *testing.T) {
	c := changed(core.FieldChange{Field: "ci", Old: "running", New: "success"})
	if IsTriageWorthy(c) {
		t.Error("CI→success should NOT be triage-worthy")
	}
}

func TestTriageWorthyConflicts(t *testing.T) {
	c := changed(core.FieldChange{Field: "conflicts", Old: false, New: true})
	if !IsTriageWorthy(c) {
		t.Error("conflicts appearing should be worthy")
	}
}

func TestTriageWorthyUnresolved(t *testing.T) {
	c := changed(core.FieldChange{Field: "unresolved", Old: false, New: true})
	if !IsTriageWorthy(c) {
		t.Error("new unresolved threads should be worthy")
	}
}

func TestTriageNotWorthyCommentBump(t *testing.T) {
	c := changed(core.FieldChange{Field: "comments", Old: 2, New: 3})
	if IsTriageWorthy(c) {
		t.Error("comment-only bump should not be worthy")
	}
}

func TestTriageNotWorthyNewOrGone(t *testing.T) {
	if IsTriageWorthy(core.Change{Kind: core.KindNew}) {
		t.Error("new MR should not be triage-worthy")
	}
	if IsTriageWorthy(core.Change{Kind: core.KindGone}) {
		t.Error("gone MR should not be triage-worthy")
	}
}
