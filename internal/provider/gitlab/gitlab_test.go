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

func TestToMRUnresolvedInversion(t *testing.T) {
	base := rawMR{
		References: struct {
			Full string `json:"full"`
		}{Full: "g/p!1"},
	}

	const pat = `([A-Z][A-Z0-9]+-\d+)`

	// BlockingOK=false → Unresolved=true
	rm := base
	rm.BlockingOK = false
	mr := toMR(rm, "me", pat)
	if !mr.Unresolved {
		t.Errorf("BlockingOK=false: want Unresolved=true, got false")
	}

	// BlockingOK=true → Unresolved=false
	rm2 := base
	rm2.BlockingOK = true
	mr2 := toMR(rm2, "me", pat)
	if mr2.Unresolved {
		t.Errorf("BlockingOK=true: want Unresolved=false, got true")
	}

	// Draft via Draft field
	rm3 := base
	rm3.Draft = true
	rm3.WIP = false
	mr3 := toMR(rm3, "me", pat)
	if !mr3.Draft {
		t.Errorf("Draft=true,WIP=false: want Draft=true, got false")
	}

	// Draft via WIP field
	rm4 := base
	rm4.Draft = false
	rm4.WIP = true
	mr4 := toMR(rm4, "me", pat)
	if !mr4.Draft {
		t.Errorf("Draft=false,WIP=true: want Draft=true, got false")
	}
}

func TestParseApprovals(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "approvals.json"))
	if err != nil {
		t.Fatal(err)
	}
	approvers, required, err := parseApprovals(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(approvers) != 1 || approvers[0] != "alice" {
		t.Errorf("approvers = %v, want [alice]", approvers)
	}
	if required != 2 {
		t.Errorf("required = %d, want 2", required)
	}
}
