package detailpane

import (
	"strings"
	"testing"

	"github.com/dmitry/mrglass/internal/core"
	"github.com/dmitry/mrglass/internal/jira"
	"github.com/dmitry/mrglass/internal/tui/theme"
)

func st() theme.Styles { return theme.BuildStyles(theme.Get("default")) }
func baseMR() core.MR {
	return core.MR{Ref: "g/p!1", Title: "t", SourceBranch: "x", TargetBranch: "main", CI: "success"}
}

func TestTicketLoading(t *testing.T) {
	out := Render(st(), baseMR(), "", TicketView{Show: true, Key: "ECFX-1", Loading: true})
	if !strings.Contains(out, "ECFX-1") || !strings.Contains(out, "loading") {
		t.Errorf("loading ticket line missing: %q", out)
	}
}

func TestTicketError(t *testing.T) {
	out := Render(st(), baseMR(), "", TicketView{Show: true, Key: "ECFX-1", Err: true})
	if !strings.Contains(out, "ECFX-1") || !strings.Contains(out, "unavailable") {
		t.Errorf("error ticket line missing: %q", out)
	}
}

func TestTicketOK(t *testing.T) {
	tv := TicketView{Show: true, Key: "ECFX-1", T: jira.Ticket{
		Key: "ECFX-1", Status: "In Review", StatusCategory: "indeterminate",
		Assignee: "Jane Smith", Summary: "Inject the thing",
	}}
	out := Render(st(), baseMR(), "", tv)
	for _, want := range []string{"ECFX-1", "In Review", "Jane Smith", "Inject the thing"} {
		if !strings.Contains(out, want) {
			t.Errorf("ticket detail missing %q in:\n%s", want, out)
		}
	}
}

func TestTicketHiddenWhenNotShown(t *testing.T) {
	out := Render(st(), baseMR(), "", TicketView{}) // Show:false
	if strings.Contains(out, "🎫") {
		t.Errorf("no ticket line should render when Show=false: %q", out)
	}
}

func TestTicketNoteShownWhenStatusOff(t *testing.T) {
	out := Render(st(), baseMR(), "", TicketView{Show: true, Key: "ECFX-1", Note: "status off: set JIRA_EMAIL + JIRA_API_TOKEN"})
	if !strings.Contains(out, "ECFX-1") || !strings.Contains(out, "JIRA_EMAIL") {
		t.Errorf("note line should explain why status is off: %q", out)
	}
}
