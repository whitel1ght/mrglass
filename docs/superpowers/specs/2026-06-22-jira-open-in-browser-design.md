# Jira: open ticket in browser (Phase 1)

_Date: 2026-06-22 · Branch: feat/jira-open-in-browser · Status: approved_

Open the Jira ticket associated with the selected MR in the browser, via a hotkey.
Zero auth, zero network, minimal config — it constructs the universal Jira
permalink and reuses the existing OS-opener.

## Background

Every `core.MR` already carries a parsed `TicketKey` (e.g. `ECFX-1234`, from the
title or source branch via `ticketRegex`); MRs with no match have `TicketKey ==
"Other"`. Every Jira ticket — Cloud or Data Center — has a canonical permalink:

```
<baseURL>/browse/<KEY>      e.g. https://ecfx.atlassian.net/browse/ECFX-1234
```

Opening that URL needs no API token and no network call from mrglass — just the
base URL and the key. mrglass already has an `openURL` helper (used by `o` for the
MR web URL) that launches the OS browser as a detached process.

This is Phase 1. Phase 2 (inline ticket status/summary via the Jira REST API +
token) is deliberately out of scope and will be its own branch.

## Requirements

- **Config**: one new field `jira.baseURL` (string), e.g. `https://ecfx.atlassian.net`.
  Trailing slash tolerated. Empty by default (feature simply prompts to configure).
- **Hotkey `J`** (capital; lowercase `j` is line-down): on the selected MR, open
  `<baseURL>/browse/<TicketKey>` in the browser.
- **Edge cases (graceful, never error):**
  - `jira.baseURL` empty → status: `⚠ set jira.baseURL in config to open tickets`.
  - MR has no ticket (`TicketKey` empty or `"Other"`) → status: `no Jira ticket on this MR`.
  - both present → open browser, status notes the ticket opened.
- Reuse the existing `openURL` helper (detached process, no terminal flash) and the
  existing `openErrMsg` failure path.

## Design

### Config (`internal/config/config.go`)
Add a `Jira` sub-struct:
```go
type JiraConfig struct {
	BaseURL string `yaml:"baseURL"`
}
// in Config:
Jira JiraConfig `yaml:"jira"`
```
`Default()` leaves `Jira.BaseURL` empty. Documented in `config.example.yaml`.

### URL construction (pure, testable)
A small helper, in `core` (alongside ticket parsing) so it is provider-agnostic
and unit-testable without the TUI:
```go
// TicketURL builds the Jira permalink for a ticket key under baseURL.
// Returns "" when baseURL is empty or key is empty/"Other".
func TicketURL(baseURL, key string) string
```
- trims a trailing `/` from baseURL,
- treats `""` and `"Other"` keys as "no ticket" → returns `""`,
- otherwise returns `baseURL + "/browse/" + key`.

### Keybinding (`internal/tui/keys/keys.go`)
Add `OpenTicket key.Binding` bound to `J` (`key.WithHelp("J", "open Jira ticket")`),
include it in `ShortHelp`/`FullHelp`.

### TUI handler (`internal/tui/app.go`)
New case in `handleKey`:
```go
case key.Matches(msg, m.keys.OpenTicket):
	mr := m.selected()
	if mr == nil { return m, nil }
	if m.cfg.Jira.BaseURL == "" {
		m.status = "⚠ set jira.baseURL in config to open tickets"; return m, nil
	}
	url := core.TicketURL(m.cfg.Jira.BaseURL, mr.TicketKey)
	if url == "" {
		m.status = "no Jira ticket on this MR"; return m, nil
	}
	m.status = "opening " + mr.TicketKey + "…"
	return m, openURL(url)
```
Reuses `openURL` (existing) and its `openErrMsg` error handling.

## Out of scope (Phase 2, later branch)
- Fetching ticket status/summary/assignee via the Jira REST API.
- Any Jira auth/token handling.
- Showing ticket info inline in the MR detail.

## Testing
- `core.TicketURL`: table-driven — normal key, trailing-slash baseURL, empty
  baseURL, empty key, `"Other"` key.
- `keys`: `OpenTicket` has a key bound and appears in help.
- `tui`: pressing `J` with (a) no baseURL → config-prompt status, no open cmd;
  (b) baseURL + ticket → returns an open command, status notes the ticket;
  (c) baseURL + no-ticket MR → "no Jira ticket" status, no open cmd.
