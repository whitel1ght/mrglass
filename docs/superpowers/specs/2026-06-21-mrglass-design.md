# mrglass — Design (v1)

_Date: 2026-06-21 · Status: approved, ready for implementation planning_

An interactive terminal dashboard (htop/mole-style) for tracking your active GitLab
merge requests, with vim-style navigation, a configurable per-MR statusline, themes,
background auto-refresh, desktop notifications, and **Claude triage**: when a
deterministic poller detects a meaningful change to an MR, Claude explains what it
means and what to do next — spending tokens only when there is something worth
reasoning about.

This is the successor to the `gitlab-mr-status` prototype (a Python script that emits
a Markdown report and a JSON change-list). `mrglass` is a fresh Go project; it **ports**
the prototype's proven change-detection logic into Go rather than shelling out to it.
The prototype stays intact as the reference implementation.

---

## 1. Goals & non-goals

### Goals (v1)

- A full-screen, interactive TUI dashboard of the user's open MRs, navigable by hotkeys.
- **Configurable sections** (each a saved filter), a **configurable per-MR statusline**,
  and **themes** — all declarative (YAML), runnable with zero config.
- A **clean provider abstraction** with a GitLab implementation (shelling out to `glab`),
  designed so a GitHub implementation slots in later without restructuring.
- **Tiered Claude integration**, token-efficient by construction:
  - Tier 0 — deterministic poll + diff (free, every tick).
  - Tier 1 — Claude **triage** of meaningful changes ("what it means / what to do"),
    surfaced in the TUI, gated by a pure-Go pre-filter.
- **Desktop notifications** on meaningful change (ported from the prototype).
- **Open-in-browser** and other hotkey actions.
- Single static binary, easy install, boring dependency management.

### Non-goals (v1 — deferred to later specs)

- **Tier 2 — agentic propose-and-confirm fix** (Claude drafts a diff addressing a
  review, you approve with one keypress, the tool pushes). Seams are left in place;
  built in spec #2.
- **GitHub provider** (spec #3). The abstraction exists in v1; the implementation does not.
- **Lua scripting.** v1 uses declarative YAML + `expr` + Go templates, which the research
  shows is sufficient and is what every comparable tool (gh-dash, lazygit, k9s) does.
  The config is designed so an optional Lua layer could be added later without a rewrite.
- Webhooks / always-on daemon. v1 is a foreground TUI; background-only notification stays
  the prototype's cron/launchd job.

### Success criteria

- `mrglass` runs with an authenticated `glab` and no config file, showing the user's open
  MRs grouped by ticket, navigable by `j/k`, openable in the browser with `o`.
- A meaningful change (e.g. CI → failed) fires a desktop notification and, when triage-worthy,
  produces a 1–3 line Claude advice in the detail pane — and idle ticks cost zero tokens.
- The dashboard never crashes on a transient `glab`/`claude` failure; it degrades and recovers.

---

## 2. Architecture & module layout

Single Go binary, **Bubble Tea (v1 line)**, structured after `gh-dash` so the provider
abstraction and the UI sections line up.

```
mrglass/
  cmd/mrglass/main.go         # entrypoint: flags, config load, tea.Program
  internal/
    provider/
      provider.go             # Provider interface (List, Enrich, ...)
      gitlab/                 # GitLabProvider — shells out to `glab api`
    core/                     # PURE: no I/O, no Bubble Tea
      model.go                # unified MR domain struct
      snapshot.go             # MR -> meaningful-fields snapshot (ported)
      diff.go                 # Diff: meaningful transitions only (ported)
      state.go                # load/save snapshot json
    tui/
      app.go                  # root Model: sections, focus, refresh tick, layout
      section/                # section.go (BaseModel) + mrsection.go
      table/                  # generic table (Row = []string)
      statusline/             # segment-list renderer (lualine-style)
      detailpane/             # right pane: MR detail + Claude advice
      keys/                   # bubbles/key bindings + help
      theme/                  # Theme struct + Styles + registry
    watch/
      watcher.go              # poll tick -> diff -> notify + triage trigger
      notify.go               # desktop notifications (osascript/notify-send)
    analyze/
      analyzer.go             # Analyzer interface (Triage(change) -> Advice)
      claudecode.go           # impl: `claude -p --output-format json` subprocess
  config/                     # example config.yaml, theme files
  docs/superpowers/specs/     # this spec
```

### Boundaries (each independently testable)

- **`provider.Provider`** — fetch/enrich MRs from a forge. Depends on the `glab` CLI.
  GitHub becomes a sibling package later behind the same interface.
- **`core`** — pure functions: snapshot + diff + state. No network, no Bubble Tea.
  This is the ported prototype logic and the highest-value test target.
- **`analyze.Analyzer`** — turn a detected change into human advice. Depends on the `claude`
  CLI. One impl in v1 (subprocess); a raw-API impl can be added without touching callers.
- **`tui`** — pure render/update. Receives data via messages; never calls the network
  directly (background `tea.Cmd`s do).
- **`watch`** — orchestrates the tick → fetch → diff → notify/triage pipeline, emitting
  Bubble Tea messages.

### Data flow

```
watch metronome tick ─► provider.List + Enrich ─► core.Diff(savedState, current)
       │                                                   │
       └── (self-reschedules,                              ▼
            separate from fetch)                        Changes ──► ChangesMsg ─► TUI
                                                            │
                                       ┌────────────────────┴───────────────┐
                                   notify (free)                    pre-filter (free, Go)
                                                                       │ triage-worthy?
                                                                  yes ─┤
                                                                       ▼
                                                        analyze Cmd ─► claude -p (Tier 1)
                                                                       ▼
                                                            AdviceMsg ─► detail pane
```

---

## 3. Data model, snapshot & diff (ported from the prototype)

### Unified MR struct (`core/model.go`)

Provider-agnostic, so GitHub maps onto the same shape later.

```go
type Role int // Mine | ReviewRequested | ToReview

type MR struct {
    Ref       string   // "group/project!177" — stable identity key
    IID       int
    ProjectID int
    Title     string
    URL       string
    Author    string
    SourceBranch, TargetBranch string

    Role      Role     // derived vs current user
    Reviewers []string

    // --- meaningful state (drives the diff) ---
    CI          string   // head pipeline status: success/failed/running/...
    PipelineURL string
    ApprovedBy  []string // genuine approvers (approved_by), NOT GitLab's approved=true
    Conflicts   bool
    Unresolved  bool     // !blocking_discussions_resolved
    Comments    int      // user_notes_count
    Draft       bool
    MergeStatus string   // detailed_merge_status

    UpdatedAt time.Time
    TicketKey string     // parsed from title/branch via configurable regex; "" -> "Other"

    approvalsOK bool     // internal: did the approvals fetch succeed this run?
}
```

### Three-bucket query (`provider/gitlab`) — unchanged from the prototype

- `scope=created_by_me` → `Mine`
- `scope=assigned_to_me`
- `scope=all&reviewer_username=<me>` → `ReviewRequested`
  (the distinct reviewer set; requires `scope=all` or review-requests on bot-authored
  MRs are silently dropped)

Unioned + de-duped by `Ref`, all filtered `state=opened&updated_after=<now-Ndays>`.
`Enrich()` fetches `/projects/{id}/merge_requests/{iid}/approvals` per MR and sets
`approvalsOK`, so a dropped connection is never mistaken for "all approvals removed".

### Snapshot (`core/snapshot.go`)

Reduce an `MR` to diff-relevant fields only, ignoring cosmetic churn (`updated_at`, etc.).
If the approvals fetch failed this run, carry forward the prior snapshot's `ApprovedBy`.

```go
type Snapshot struct {
    Ref, Title, URL string
    CI          string
    ApprovedBy  []string  // sorted
    Conflicts   bool
    Unresolved  bool
    Comments    int
    Draft       bool
    MergeStatus string
}
```

State persists as `{ref: Snapshot}` JSON in `.mrglass-state.json` (same model as the
prototype's `.mr-state.json`).

### Diff (`core/diff.go`)

`Diff(prev, curr map[string]Snapshot) []Change` — emits **only meaningful transitions**:

```go
type ChangeKind int // New | Gone | Changed

type FieldChange struct {
    Field    string      // "ci", "approved_by", "conflicts", ...
    Old, New interface{}
}

type Change struct {
    Ref, URL, Title string
    Kind   ChangeKind
    Detail string        // human string, DERIVED from Fields
    Fields []FieldChange // structured: the source of truth
}
```

Meaningful set (unchanged from the prototype):

- **New** in scope
- **Gone** (merged / closed / aged out)
- **CI** status flip
- **Approval** gained / lost
- **Conflicts** appeared / cleared
- **Unresolved threads** appeared / cleared
- **+N comments**
- **Draft ↔ ready**

First run establishes a baseline and reports zero changes.

**Deliberate addition over the prototype:** each `Change` carries structured `Fields`
alongside the human `Detail` string. The prototype produced only the string. The TUI needs
the structure to color rows; the pre-filter (§4) needs it to decide whether a change is even
worth a Claude call. `Detail` is rendered from `Fields`, so there is one source of truth.

`core` is pure: tested with JSON fixtures lifted from a real `.mr-state.json` — every
meaningful-transition case, the first-run baseline, and the approvals-carry-forward guard.

---

## 4. Change detection → notify → Claude triage (token tiers)

Discipline: **the expensive tier runs only when the cheap tier found something.**

### Tier 0 — deterministic poll + diff (free, every tick, no tokens)

A self-rescheduling `tea.Tick` **metronome** (interval configurable; default 5 min,
`0` = manual-only), kept **separate** from the fetch `Cmd` so a slow `glab` call never
delays the next tick. Each tick: `provider.List → Enrich → core.Diff(savedState, current)`.
Results enter the Bubble Tea loop as `ChangesMsg`, guarded by a `LastFetchID` so a stale
in-flight fetch (e.g. after a filter change) is discarded. Idle ticks cost only a few
`glab` calls.

### Tier 1 — Claude triage (cheap, only on meaningful change)

For each `Change` the differ emits, the watcher:

1. **Notifies** immediately (free, deterministic — ported from `mr-notify.py`).
2. Applies a **pure-Go pre-filter** before spending any tokens. Triage-worthy:
   CI → failed, conflicts appeared, a reviewer's unresolved thread, approval lost.
   Not triage-worthy (notify only): bot comment, your own draft toggle, cosmetic
   merge-status flips. **This pre-filter is the biggest token lever — and it is plain Go,
   no Claude call to decide whether to call Claude.**
3. For triage-worthy changes, fires an async `analyze` Cmd:
   `claude -p --output-format json --allowedTools Read --bare`, with a tight prompt (the
   structured change + minimal MR context) asking for a 1–3 line "what it means / what to do
   next." The already-registered GitLab MCP server is available so Claude *can* pull a
   pipeline log or thread to ground its advice — but only on this rare path, never on the tick.
4. The resulting `AdviceMsg{ref, advice}` lands in the detail pane next to that MR; the row
   gets a `💡` marker.

### Tier 2 — agentic fix (NOT in v1)

Reserved: "draft a diff that addresses the review, show it, push only on `[y]`." v1 leaves a
hotkey stub and a clean `Analyzer`/action seam so spec #2 adds it without restructuring.

### Concurrency & cost guards

- Triage Cmds are **debounced and capped** — at most N in flight, plus a per-MR cooldown so
  a chatty MR can't trigger a triage storm.
- Each triage records `{ref, change-hash}`; the **same** change is never re-triaged across
  restarts.
- A visible toggle (`a`) disables auto-triage entirely → pure dashboard + notify, zero Claude.
  Manual triage of the selected MR (`t`) is always available, so spend stays under user control.

### Auth / runtime

Triage shells out to the user's existing **Claude Code login** — no API key, no separate
billing. If `claude` is not on `PATH`, Tier 1 silently degrades to notify-only and the
dashboard works fully without it (mirroring how the prototype degrades without the MCP server).
The `Analyzer` interface keeps a future raw-Anthropic-API impl a drop-in.

---

## 5. TUI surface

### Layout (gh-dash-shaped)

```
┌ tabs: [Needs My Review] [Mine] [Approved & Green] ────────────────┐
│ ABC-1234                                          ╎ MR DETAIL      │
│  ▸ group/proj!177  feat: new thing   ✗CI 1/2✓ 💬2 ╎ title, branch  │
│    group/proj!178  fix: thing        ✓CI 2/2✓    ╎ review/CI/merge │
│ ABC-9                                             ╎ ─────────────  │
│    group/proj!172  chore: bump       🔄CI         ╎ 💡 Claude: CI   │
│                                                   ╎ failed on lint; │
│                                                   ╎ run `make fmt`  │
├ status: 5 MRs · refreshed 14:21 · auto-triage ON ─────────────────┤
│ ?:help j/k:move enter:detail o:open r:refresh t:triage a:auto      │
└───────────────────────────────────────────────────────────────────┘
```

Left = ticket-grouped MR list (a generic `table.Model`, `Row = []string`). Right = detail
pane for the selected MR, including Claude advice. Bottom = status bar + context help.
`JoinHorizontal(list, detail)` over `JoinVertical(tabs, …, footer)`.

Sections-as-interface + a shared `BaseModel` (gh-dash pattern): the GitLab-MR section and
the future GitHub-PR section embed one base and differ only in fetch + row-build.

### Sections = saved filters (configurable)

Each tab is an [`expr-lang/expr`](https://github.com/expr-lang/expr) predicate over the `MR`
struct. Defaults ship sensible:

```yaml
sections:
  - title: "Needs My Review"
    filter: 'role == "review_requested" && !draft'
  - title: "Mine"
    filter: 'role == "mine"'
  - title: "Approved & Green"
    filter: 'len(approvedBy) > 0 && ci == "success"'
```

An MR's **disappear criteria** are implicit: it leaves a section when it stops matching
(merged → out of scope → drops out) — the "disappear when merged" requirement expressed as
data, not special-cased.

### Per-MR statusline = a typed segment list (lualine-style)

Left/right groups, a **row-state axis** (selected / ci_failed / stale) that restyles rows,
and per-segment `when` predicates:

```yaml
statusline:
  states:
    selected:  { bg: "#2a2a40", bold: true }
    ci_failed: { fg: "#e06c75" }
  left:
    - { type: marker, source: role }            # ▸ mine / ◇ to-review
    - { type: text,   source: title, grow: true, maxWidth: 60 }
  right:
    - { type: ci,        when: 'ci != ""',
        symbols: { success: "✓", failed: "✗", running: "🔄" } }
    - { type: approvals, when: 'required > 0', format: "{approved}/{required}✓" }
    - { type: comments,  when: 'comments > 0', format: "💬{comments}" }
    - { type: advice,    when: 'hasAdvice',    text: "💡" }
    - { type: age,       source: updatedAt, align: right, style: faint }
```

Each entry deserializes to a `Segment` struct; the renderer maps `type` to a built-in
value-producer, applies `when` (an `expr`), and styles via the theme. This is the
customizable statusline requirement in declarative form — and the seam where an optional Lua
layer could later replace a segment's value-producer without changing the renderer.

### Keybindings (`bubbles/key` + `bubbles/help` overlay on `?`)

Vim-style defaults, rebindable in config:

| Key | Action |
|---|---|
| `j/k`, `g/G` | navigate MRs / top-bottom |
| `h/l` or `tab` | switch sections/tabs |
| `enter` | focus detail pane |
| `o` | **open MR in browser** (`tea.ExecProcess` → `open`/`xdg-open` on `URL`) |
| `r` | refresh now (manual tick) |
| `t` | triage selected MR now (manual Tier-1) |
| `a` | toggle auto-triage on/off |
| `?` / `q` | help overlay / quit |

Custom actions follow gh-dash's pattern: a config `command:` templated against the MR
(`glab mr ... {{.IID}}`) run via `tea.ExecProcess`.

### Theming

A `Theme` struct of `AdaptiveColor`s → a construct-once `Styles` struct (built at startup,
never in `View()`), selectable from a named registry (`default`, `dracula`, …) or a user
theme file.

### Config resolution

Built-in defaults → `~/.config/mrglass/config.yaml` → `--flag`/env. Everything has a working
default; the tool runs with an empty config.

---

## 6. Error handling — degrade, never crash the UI

A dashboard watched all day must never die on a transient failure.

- **`glab` transport errors** (dropped keep-alive / "EOF" class) → retry with backoff in the
  provider; on persistent failure, keep the last-known list, mark the status bar
  `⚠ refresh failed 14:21, retrying`, recover on the next tick. Never blank the list.
- **Approvals fetch fails for one MR** → carry forward prior `ApprovedBy` (`approvalsOK`
  guard); a blip never renders as "approval removed" or fires a false notification.
- **`claude` missing / triage fails / times out** → Tier 1 degrades to notify-only; no `💡`,
  status bar notes `triage unavailable`. Dashboard fully usable with zero Claude.
- **`glab` not authenticated at startup** → one clear blocking screen ("run `glab auth login`"),
  not a stack trace.
- **Malformed config / bad `expr` filter / unknown segment type** → fail loud but localized:
  load defaults for the broken piece, surface the parse error in a startup banner, keep running.
- Every background `Cmd` returns errors **as messages** (`fetchErrMsg`, `triageErrMsg`) handled
  in `Update` — no `panic`, no goroutine mutating UI state directly.

---

## 7. Testing strategy

Maps to the isolation boundaries:

- **`core`** (snapshot/diff/state) — pure unit tests with JSON fixtures lifted from a real
  `.mr-state.json`. Highest value: every meaningful-transition case, first-run baseline,
  approvals-carry-forward guard.
- **`provider/gitlab`** — tests against recorded `glab` JSON fixtures; no live network.
- **`statusline` + `expr` filters** — table-driven: given an `MR` + segment/filter config,
  assert rendered string / predicate result.
- **`analyze`** — `Analyzer` is an interface; the TUI is tested with a fake analyzer returning
  canned advice. The real `claudecode` impl gets a thin subprocess-contract test (parses a
  known `claude -p` JSON shape).
- **`tui`** — [`teatest`](https://github.com/charmbracelet/x/tree/main/exp/teatest): send key
  msgs, assert view output; that a `ChangesMsg` updates rows and an `AdviceMsg` reaches the
  detail pane.

---

## 8. Distribution & dependency management

- **Dependencies**: standard Go modules — commit `go.mod` + `go.sum`, `go mod tidy` after
  changes, **no vendoring, no workspaces**. Pin the Charm libs and **stay on the stable Bubble
  Tea v1 line** (v2 is mid-migration with breaking import-path changes). Grouped **Dependabot**
  so all `charmbracelet/*` bumps land in one PR.
- **Shipping**: **GoReleaser** → cross-compiled static binaries (`CGO_ENABLED=0`,
  linux/darwin/windows × amd64/arm64) on every `v*` tag via GitHub Actions → GitHub Releases,
  plus a **Homebrew cask** tap and `go install`. Single static binary, no runtime deps of its
  own.
- **Runtime prerequisites** (checked at startup): `glab` (authenticated) required; `claude`
  optional (Tier 1); a notifier (`osascript`/`notify-send`) optional.

---

## 9. Key libraries

| Concern | Library |
|---|---|
| TUI runtime | `github.com/charmbracelet/bubbletea` (v1 line) |
| Styling | `github.com/charmbracelet/lipgloss` |
| Widgets (table, key, help, spinner) | `github.com/charmbracelet/bubbles` |
| Filters / predicates | `github.com/expr-lang/expr` |
| Statusline / action templates | stdlib `text/template` |
| Config | YAML via `koanf` or `gopkg.in/yaml.v3` (decide at planning) |
| TUI tests | `github.com/charmbracelet/x/exp/teatest` |
| Release | GoReleaser |

---

## 10. Roadmap (subsequent specs)

- **Spec #2 — Tier 2 agentic propose-and-confirm fix**: Claude drafts a diff addressing a
  review (`claude -p` with `Read,Edit`, no push), the TUI shows it, the tool pushes only on
  user confirmation. The push is performed by `mrglass`, not granted to the model — the human
  gate is enforced by withholding the tool, not by trusting the model.
- **Spec #3 — GitHub provider**: a `GitHubProvider` (shelling out to `gh`) behind the existing
  interface; unified dashboard across both forges.
- **Spec #4 — optional Lua scripting**: if programmable config becomes a headline feature,
  add an optional Lua layer (`yuin/gopher-lua` + `gopher-luar`) at the statusline value-producer
  and custom-action seams — without disturbing the declarative defaults.
