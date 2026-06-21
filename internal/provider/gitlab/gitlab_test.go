package gitlab

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dmitry/mrglass/internal/core"
)

func roleMine() core.Role { return core.RoleMine }

func TestToMRMapsFields(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "mrs.json"))
	if err != nil {
		t.Fatal(err)
	}
	list, err := parseMRList(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 MR, got %d", len(list))
	}
	mr := toMR(list[0], "you", `([A-Z][A-Z0-9]+-\d+)`)
	if mr.Ref != "group/project!177" {
		t.Errorf("Ref = %q", mr.Ref)
	}
	if mr.IID != 177 || mr.ProjectID != 42 {
		t.Errorf("IID/ProjectID = %d/%d", mr.IID, mr.ProjectID)
	}
	if mr.CI != "failed" {
		t.Errorf("CI = %q, want failed", mr.CI)
	}
	if mr.PipelineURL == "" {
		t.Error("PipelineURL should be set")
	}
	if mr.Comments != 2 {
		t.Errorf("Comments = %d", mr.Comments)
	}
	if mr.TicketKey != "ABC-1234" {
		t.Errorf("TicketKey = %q", mr.TicketKey)
	}
	if mr.Role != roleMine() {
		t.Errorf("author==me should be RoleMine, got %v", mr.Role)
	}
}

func TestParseApprovals(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "approvals.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseApprovers(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 || got[0] != "alice" {
		t.Errorf("approvers = %v, want [alice]", got)
	}
}
