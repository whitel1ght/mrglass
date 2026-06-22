# Configurable providers: GitHub forge + generic ticket-open

_Date: 2026-06-22 · Branch: feat/configurable-providers · Status: approved_

Make mrglass usable with **GitHub or GitLab** (config-selected) and **any ticket
system** for open-in-browser (config URL template). Remove the GitLab/Jira
hardcoding so additional forges/trackers are drop-ins. Inline ticket *status*
stays Jira-only for now (the one tracker with an implemented API client).

## Goals / non-goals

Goals:
1. **Forge selection by config**: `forge: gitlab | github`. Add a GitHub provider
   behind the existing `provider.Provider` interface; `main` picks by config.
2. **Generic ticket open**: replace `jira.baseURL`-specific URL building with a
   configurable `tickets.urlTemplate` (e.g. `https://ecfxdev.atlassian.net/browse/{key}`,
   or `https://linear.app/acme/issue/{key}`, or a GitHub-issues URL). Works for
   ANY tracker with zero per-tracker code. `J` uses it.
3. Keep everything working: list/diff/triage/review for GitLab; open-in-browser
   for any tracker; inline status for Jira.

Non-goals (later, documented):
- GitHub *review* feature parity (diff/post/worktree via `gh`) — Phase-2-of-this.
  v1: review feature is available on GitLab; on GitHub the `c` review is disabled
  with a clear status until the `gh` path is built.
- Inline ticket status for non-Jira trackers (needs per-tracker API clients).

## What's hardcoded today (to remove)

- `cmd/mrglass/main.go`: `gitlab.New()` always; Jira client built from `jira.baseURL`.
- `internal/core/model.go`: `TicketURL` assumes Jira's `/browse/<KEY>` path.
- `internal/config/config.go`: `jira.baseURL` field.
- `internal/tui`: `ticketView`/handlers reference `cfg.Jira.BaseURL`.

## Design

### 1. Config
```yaml
forge: gitlab          # gitlab | github   (default: gitlab)

tickets:
  # {key} is replaced with the ticket key (e.g. ECFX-9340). Empty = ticket open disabled.
  urlTemplate: "https://ecfxdev.atlassian.net/browse/{key}"
  # inline status (Jira only for now); needs JIRA_EMAIL/JIRA_API_TOKEN in env
  status: jira          # jira | none      (default: none)
  jiraBaseURL: "https://ecfxdev.atlassian.net"   # used only when status: jira
```
- New `Forge string` (default "gitlab").
- New `Tickets TicketsConfig { URLTemplate, Status, JiraBaseURL string }`.
- **Migration**: the old `jira.baseURL` is gone. `config.Load` maps a legacy
  `jira.baseURL` (if present and `tickets` absent) into `tickets.urlTemplate =
  "<jira.baseURL>/browse/{key}"` and `tickets.jiraBaseURL`, with a one-line
  warning, so existing configs keep working. (Implement as a post-unmarshal
  fixup.)

### 2. Generic ticket URL (`internal/core/model.go`)
Replace `TicketURL(baseURL, key)` with template-based:
```go
// TicketURL renders a ticket key into urlTemplate by replacing "{key}".
// Returns "" when the template is empty or the key is empty/"Other".
func TicketURL(urlTemplate, key string) string {
	if urlTemplate == "" || key == "" || key == "Other" {
		return ""
	}
	return strings.ReplaceAll(urlTemplate, "{key}", key)
}
```
Callers pass `cfg.Tickets.URLTemplate` instead of a Jira base URL.

### 3. Forge provider selection
The `provider.Provider` interface already abstracts list/whoami; the review
feature additionally needs diff/post (today via the concrete gitlab type). Define
the review forge capability as an interface the provider satisfies (it already is
`review.GitLab` shape):
- `main` builds the provider by `cfg.Forge`:
  - `gitlab` → `gitlab.New()` (existing).
  - `github` → `github.New()` (NEW): shells out to `gh` (`gh api`, `gh pr` …),
    implements `provider.Provider` (Whoami, List with the 3 buckets' GitHub
    analogs: `author:@me`, `assignee:@me`, `review-requested:@me`), maps PRs to
    `core.MR` (reviews→ApprovedBy, checks→CI, mergeable→Conflicts, etc.).
- **Review wiring**: only attach `WithReview` when the provider supports it. GitLab
  does (MRDiff/PostNote exist). For GitHub v1, EITHER implement the gh-based
  diff/post too OR gate `c` off on GitHub with "review not yet supported on
  github". Decision below.

### 4. GitHub provider (`internal/provider/github`)
- `Runner`/exec wrapper around `gh` (mirror gitlab's `glab.go`): `gh api <path>`.
- `List`: GitHub search API or `gh pr list`. Buckets:
  - authored: `is:open is:pr author:@me`
  - assigned: `is:open is:pr assignee:@me`
  - review-requested: `is:open is:pr review-requested:@me`
  Union/dedupe by a stable ref (`owner/repo#123`).
- Map a PR JSON → `core.MR`: title, url, author, head/base refs, draft,
  `reviewDecision`/`reviews` → ApprovedBy + ApprovalsRequired, check-runs → CI,
  `mergeable`/`mergeStateStatus` → Conflicts, comments count, updated_at, ticket
  via `ParseTicket(title, headRef, regex)`.
- Diff/post for review: `gh pr diff <n>` and `gh pr comment <n> --body` — wire
  these into the review.GitLab-shaped capability so `c` works on GitHub too IF we
  choose to (see decision).

### 5. TUI
- `J` uses `core.TicketURL(cfg.Tickets.URLTemplate, mr.TicketKey)`; messaging
  updated ("set tickets.urlTemplate…").
- Inline status: gated on `cfg.Tickets.Status == "jira"` AND jira env creds; uses
  `cfg.Tickets.JiraBaseURL`. Unchanged behavior otherwise.
- Status/help copy stays forge-neutral where shown ("MR/PR" is fine to keep as
  "MR" in v1 to limit churn; note as cosmetic follow-up).

## Decision points folded in
- **GitHub review (`c`) in v1**: implement gh-based diff + comment so `c` works on
  GitHub too (keeps feature parity; it's the same review pipeline, just a
  different forge capability). The worktree/local-context path already keys off a
  local clone + branch; GitHub MR-head fetch is `gh pr checkout`-style or
  `refs/pull/<n>/head` — reuse the same worktree approach with a github fetch ref.
- **Provider review capability** expressed as an interface (`ReviewForge` =
  MRDiff + PostNote) that both gitlab and github providers implement; `main`
  passes whichever provider in as `review.GitLab`.

## Testing
- `core.TicketURL`: template replace, empty template, empty/"Other" key, a
  non-Jira template (Linear) to prove genericity.
- `config`: forge default; tickets parsing; legacy `jira.baseURL` migration.
- `provider/github`: PR-list JSON fixture → core.MR mapping (reviews→approvals,
  checks→CI, mergeable→conflicts); 3-bucket query args; diff/comment arg shapes —
  all via a fake `gh` runner (no network).
- `main`/wiring: forge=github builds the github provider; review capability wired
  for both.
- `tui`: J uses the template; inline status still gated on jira config.

## Rollout note
This is a large change landed on one branch (`feat/configurable-providers`) per
request; built incrementally with the suite green at each step.
