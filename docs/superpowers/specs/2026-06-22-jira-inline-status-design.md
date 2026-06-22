# Jira: inline ticket status (Phase 2)

_Date: 2026-06-22 · Branch: feat/jira-inline-status · Status: approved_

Show the Jira ticket's status, assignee, and summary inline in the expanded MR
detail, fetched lazily from the Jira REST API and cached. Builds on Phase 1
(open-in-browser, `jira.baseURL`).

## Decisions

- **Auth**: Jira Cloud Basic auth — `JIRA_EMAIL` + `JIRA_API_TOKEN` from the
  environment (no secrets in config; matches `jira-cli`'s convention). `baseURL`
  comes from `config.jira.baseURL` (already present). If baseURL OR either env
  var is missing, Phase 2 is **silently disabled** — the MR detail simply omits
  ticket info, and Phase 1 (`J` open-in-browser) keeps working.
- **Fetch**: **lazy on expand.** When an MR row is expanded (`enter`) and its
  ticket isn't cached, fire one async fetch. Cache per ticket key with a TTL so
  re-expanding / tab-switching doesn't refetch.
- **Display**: in the expanded detail, a line
  `🎫 PROJ-1234 · In Review · Jane Smith` colored by status **category**
  (To Do→subtle, In Progress→accent, Done→green), plus the summary on the next
  line. While fetching: `🎫 PROJ-1234 · loading…`. On error: `🎫 PROJ-1234 ·
  (status unavailable)` — never blocks the rest of the detail.

## API

Jira Cloud REST v3 (Data Center would be `/rest/api/2/`; v1 targets Cloud):
```
GET <baseURL>/rest/api/3/issue/<KEY>?fields=summary,status,assignee
Authorization: Basic base64(email:token)   (Go: req.SetBasicAuth(email, token))
Accept: application/json
```
Response (trimmed):
```json
{ "key": "PROJ-1234",
  "fields": {
    "summary": "Inject PROCESSOR_LOGGING_BUCKET",
    "status":   { "name": "In Review", "statusCategory": { "key": "indeterminate" } },
    "assignee": { "displayName": "Jane Smith" } } }
```
`statusCategory.key` ∈ {`new` (To Do), `indeterminate` (In Progress), `done`}.
assignee may be null (→ "Unassigned"). On Cloud, a bad token often yields **404**
on an issue lookup, not 401 — treat any non-2xx as "status unavailable".

## Architecture

### New package `internal/jira`
```go
type Ticket struct {
	Key, Summary, Status, StatusCategory, Assignee string
}
type Client interface { Fetch(key string) (Ticket, error) }

// HTTPClient implements Client against the Jira REST API.
type HTTPClient struct { BaseURL, Email, Token string; HTTP *http.Client }
func (c HTTPClient) Fetch(key string) (Ticket, error)  // GET issue, map fields

// Configured reports whether baseURL+email+token are all present.
func Configured(baseURL, email, token string) bool
// FromEnv reads JIRA_EMAIL / JIRA_API_TOKEN.
func FromEnv() (email, token string)
```
Pure mapping (`parseIssue([]byte) (Ticket, error)`) is unit-tested with a fixture;
the HTTP layer takes an injectable `*http.Client` (or a small `doer` interface) so
`Fetch` is testable with a fake transport — no network in tests. Assignee nil →
"Unassigned".

### TUI wiring (`internal/tui/app.go`)
- Model gains: `jira jira.Client` (nil when unconfigured), `tickets map[string]jira.Ticket`,
  `ticketErr map[string]bool`, `ticketFetching map[string]bool`, and a TTL stamp
  per key (reuse a simple `fetchedAt map[string]time.Time`).
- On **expand** (the existing `Expand` key handler): if `m.jira != nil` and the
  MR has a real ticket (`core.TicketURL(...) != ""` ⇒ key is real) and it's not
  cached/fresh/in-flight, mark fetching and return a `jiraFetchCmd(key)`.
- `jiraFetchCmd(key)` → async `tea.Cmd` → `jiraMsg{key, ticket, err}`.
- `jiraMsg` handler: store ticket or mark error; clear fetching; stamp time.
- Pass the ticket (if any) + its state into `detailpane.Render`.

### detailpane (`internal/tui/detailpane/detailpane.go`)
`Render` gains a ticket arg (a small `TicketView{Key string; Loading bool; Err bool;
T jira.Ticket}`), rendered as the `🎫` line(s) when the MR has a ticket key:
- loading → `🎫 KEY · loading…` (subtle)
- error → `🎫 KEY · (status unavailable)` (subtle)
- ok → `🎫 KEY · <Status> · <Assignee>` (status colored by category) + summary line.
If the MR has no ticket, render nothing ticket-related (unchanged behavior).

### main (`cmd/mrglass/main.go`)
Build the Jira client iff configured:
```go
email, token := jira.FromEnv()
if jira.Configured(cfg.Jira.BaseURL, email, token) {
	m = m.WithJira(jira.HTTPClient{BaseURL: cfg.Jira.BaseURL, Email: email, Token: token, HTTP: http.DefaultClient})
}
```
`WithJira` is a setter like `WithReview`. Nil client ⇒ feature off.

## Cache / TTL
Per-key cache with a TTL (default 5 min). On expand, refetch only if not cached or
stale. No background refresh in v1 (keep it lazy). A manual refresh (`r`) clears
the ticket cache so the next expand refetches.

## Out of scope
- Posting/transitioning tickets (read-only).
- Showing ticket info on the collapsed row (detail-only for now).
- Data Center `/rest/api/2/` auto-detection (v1 targets Cloud v3; document the
  limitation — a later tweak can switch the path by config).

## Testing
- `jira.parseIssue`: fixture → Ticket (status, category, assignee, summary);
  null assignee → "Unassigned".
- `jira.Fetch`: fake HTTP doer returns the fixture (2xx) → Ticket; non-2xx → error.
- `jira.Configured`/`FromEnv`: env present/absent.
- `tui`: expanding an MR with a ticket + configured client fires a fetch cmd and
  is not refetched when cached; `jiraMsg` populates the cache; detail shows the
  status line; unconfigured client → no fetch, detail omits ticket info; MR with
  no ticket → no fetch.
- `detailpane`: loading / error / ok rendering.
