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
