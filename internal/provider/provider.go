// Package provider abstracts a code-forge (GitLab, later GitHub) behind a
// uniform interface returning core.MR values.
package provider

import "github.com/whitel1ght/mrglass/internal/core"

// Provider fetches merge/pull requests from a forge for the current user.
type Provider interface {
	// Whoami returns the authenticated user's username.
	Whoami() (string, error)
	// List returns fully-enriched open MRs (authored, assigned, or
	// review-requested) updated within the last `days`, with Role and
	// TicketKey populated. ticketPattern is the ticket-key regex.
	List(me string, days int, ticketPattern string) ([]core.MR, error)
}
