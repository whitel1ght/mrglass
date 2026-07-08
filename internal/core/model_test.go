package core

import "testing"

func TestRoleString(t *testing.T) {
	cases := map[Role]string{
		RoleMine:            "mine",
		RoleReviewRequested: "review_requested",
		RoleToReview:        "to_review",
	}
	for r, want := range cases {
		if got := r.String(); got != want {
			t.Errorf("Role(%d).String() = %q, want %q", r, got, want)
		}
	}
}

func TestParseTicket(t *testing.T) {
	const pat = `([A-Z][A-Z0-9]+-\d+)`
	cases := []struct {
		title, branch, want string
	}{
		{"feat(ABC-1234): thing", "user/whatever", "ABC-1234"},
		{"no ticket here", "feature/proj-99-fix", "Other"}, // lowercase branch, no match
		{"no ticket here", "feature/PROJ-99-fix", "PROJ-99"},
		{"nothing", "nothing", "Other"},
	}
	for _, c := range cases {
		if got := ParseTicket(c.title, c.branch, pat); got != c.want {
			t.Errorf("ParseTicket(%q,%q) = %q, want %q", c.title, c.branch, got, c.want)
		}
	}
}

func TestApproved(t *testing.T) {
	cases := []struct {
		name     string
		by       []string
		required int
		want     bool
	}{
		{"required met", []string{"a", "b"}, 2, true},
		{"required exceeded", []string{"a", "b", "c"}, 2, true},
		{"required unmet", []string{"a"}, 2, false},
		{"required none, someone approved", []string{"a"}, 0, true},
		{"required none, nobody approved", nil, 0, false},
		{"required set, nobody approved", nil, 1, false},
	}
	for _, c := range cases {
		if got := Approved(c.by, c.required); got != c.want {
			t.Errorf("%s: Approved(%v,%d)=%v want %v", c.name, c.by, c.required, got, c.want)
		}
	}
}

func TestTicketURL(t *testing.T) {
	cases := []struct {
		tmpl, key, want string
	}{
		// Jira
		{"https://acme.atlassian.net/browse/{key}", "PROJ-9340", "https://acme.atlassian.net/browse/PROJ-9340"},
		// Linear — proves the template is tracker-agnostic
		{"https://linear.app/acme/issue/{key}", "ENG-12", "https://linear.app/acme/issue/ENG-12"},
		// GitHub issues
		{"https://github.com/acme/repo/issues/{key}", "42", "https://github.com/acme/repo/issues/42"},
		{"", "PROJ-1", ""},                      // no template -> nothing
		{"https://x/browse/{key}", "", ""},      // no key -> nothing
		{"https://x/browse/{key}", "Other", ""}, // "Other" = no ticket
	}
	for _, c := range cases {
		if got := TicketURL(c.tmpl, c.key); got != c.want {
			t.Errorf("TicketURL(%q,%q) = %q, want %q", c.tmpl, c.key, got, c.want)
		}
	}
}
