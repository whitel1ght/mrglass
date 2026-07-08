# mrglass — Quality-Pass Fixes (Design)

_Date: 2026-07-08 · Status: approved_

A quality pass addressing the weaknesses found in the 2026-07-08 codebase review:
formatting/dead code, robustness gaps, declared-but-unwired statusline config,
provider asymmetry, performance, GitLab test coverage, and unbounded worktree
growth. No new user-facing features except `mrglass --gc`.

**Work structure:** a single `fix/quality-pass` branch, one logical commit per
group below (hygiene → robustness → statusline → perf/coverage → gc). Every
behavioral change is test-driven; hygiene changes are verified by
`gofmt -l` + the existing suite.

---

## 1. Hygiene (no behavior change)

- `gofmt -w` the 6 unformatted files (`cmd/mrglass/main_test.go`,
  `internal/core/model.go`, `internal/core/model_test.go`,
  `internal/review/review_test.go`, `internal/tui/app.go`,
  `internal/tui/app_test.go`). `main_test.go` gets fully idiomatic layout
  (imports one-per-line, blank lines between funcs).
- Delete dead code:
  - `parseApprovers` (gitlab.go) — unused backward-compat wrapper.
  - `var _ = core.MR{}` no-ops in gitlab.go and github.go.
  - The duplicated `MRDiff` doc comment in github.go.

**Acceptance:** `gofmt -l .` is empty; `go vet ./...` clean; all tests pass.

## 2. Robustness

1. **ticketRegex validation.** Validate with `regexp.Compile` in
   `config.normalize()`; an invalid pattern → warning + fall back to the
   default pattern. `core.ParseTicket` stops calling `regexp.MustCompile`
   per call: the compiled `*regexp.Regexp` is cached (package-level cache
   keyed by pattern). Fixes both the panic-on-bad-config and the
   per-MR-per-refresh recompile.
2. **Numeric validation** in `normalize()`: `Days <= 0` → 30 (warn);
   `RefreshMinutes < 0` → 0, i.e. auto-refresh disabled (warn). Keeps the
   "config never errors" philosophy.
3. **State persistence errors surfaced.** `watch.Fetch` no longer swallows
   `SaveState` errors: the error is carried in `FetchResult` as a non-fatal
   warning and shown in the TUI status footer. `LoadState` distinguishes a
   *corrupt* state file (warn, treat as first run) from a *missing* one
   (silent first run).
4. **GitHub transient retry parity.** Extract the retry + `isTransient`
   logic from `glab.go` into a shared helper (`internal/provider/execx`)
   used by both runners. GitHub read calls (`search`, `pr view`, `api user`)
   get the same 2-retry linear backoff. Writes (`pr comment`, GitLab
   `APIPost`) never retry — duplicate-comment safety is preserved.

**Acceptance:** table tests for each rule; a bad regex in config renders the
dashboard with the default pattern and a visible warning instead of panicking.

## 3. Statusline config wiring

Placement already comes from the `left:`/`right:` segment lists, so the
per-segment `align` field is redundant by construction → **removed** from
`Segment` and the defaults. The other fields are wired:

1. **`Segment.Style`** (named style; the default `age` segment uses
   `style: "faint"`). `configStyle()` resolves names → theme styles:
   `base, subtle, faint, accent, success, warn, danger, advice`
   (`faint` = Subtle + lipgloss faint). Unknown name → keep the semantic
   default, never error. Precedence: `Styles` map (existing, ci) →
   `Style` name → semantic default.
2. **`Segment.Grow`** (default `title` has `grow: true`). The grow segment
   absorbs width pressure: if the assembled row exceeds the terminal width,
   the grow segment is re-truncated so the row fits (today the gap clamps
   to 1 and the row overflows/wraps on narrow terminals). Exactly one grow
   segment is honored — the first found.
3. **`StatuslineConfig.States`** (defaults define `selected`, `ci_failed`).
   Row-level overrides in `Render`: `States["selected"]` (when set) replaces
   the theme's `st.Selected` bar; `States["ci_failed"]` applies to rows with
   `mr.CI == "failed"` (selection wrap still applied on top). Only these two
   state keys are supported; documented in `config.example.yaml`.

Truncation logic (`MaxWidth` and Grow re-truncation) moves to one shared
helper.

**Acceptance:** statusline table tests — style name honored, unknown name
ignored, grow rows never exceed width, both States overrides; config test that
`align` is gone; `config.example.yaml` updated to match reality.

## 4. Performance + GitLab coverage

1. **Concurrent enrichment (N+1 fix).** Both providers run per-MR `enrich`
   through a bounded worker pool — plain goroutines + semaphore channel,
   limit 4 (polite to forge APIs; no new dependency). Order-preserving
   (results written by index). Per-MR enrich failures stay non-fatal,
   exactly as today.
2. **Expr program caching.** Compiled `expr` programs cached in a
   package-level `sync.Map` keyed by expression string, in
   `statusline.evalBool` and `section.Match` (env shape is fixed per call
   site, so the key is safe).
3. **Section filter bug.** `section.Filter` hardcodes `approvalsRequired=0`;
   fix to pass the MR's real `ApprovalsRequired`.
4. **GitLab provider tests.** Add a `fakeRunner` (mirroring the GitHub
   suite) covering: `List` 3-bucket dedupe, enrich-failure degradation,
   `Whoami`, `MRDiff` concatenation, `PostNote` args, and the shared
   retry/`isTransient` path. Target coverage parity with GitHub (~78%).

**Acceptance:** race-detector-clean (`go test -race`) enrichment tests with a
slow fake runner; expr cache hit test; gitlab coverage ≥ 75%.

## 5. Worktree GC — `mrglass --gc`

One-shot CLI mode; runs instead of the TUI.

1. **Discover.** Walk `projectsDir` one level deep for git repos with a
   `.mrglass-worktrees` sibling, plus `worktree.dir` when configured. For
   each repo, `git worktree list --porcelain`, keeping entries under the
   mrglass worktree root.
2. **Classify.** The slug can't be mapped back to an MR IID directly (it may
   embed a ticket key, not the IID), so classification goes the other way:
   fetch the user's open MRs via the existing `Provider.List` with a very
   large `days` window (to defeat the updated-after filter), compute
   `worktree.Slug` for each, and a worktree whose slug is **not** in that
   set is a removal candidate (its MR is merged/closed/gone). A worktree
   with uncommitted changes or unpushed commits is **never** auto-selected —
   printed as `[dirty]` and skipped.
3. **Confirm & remove.** Print the plan (`path — MR !123 merged`), one
   `y/N` prompt for the batch, then `git worktree remove`,
   `git branch -D mrglass/<slug>`, `git worktree prune`.
4. **`--gc --dry-run`** prints the plan and exits without prompting.

GC logic lives in `internal/worktree` behind the existing `GitRunner` seam
plus a small forge-lookup interface; `main.go` wiring stays thin.

**Acceptance:** table tests for classification (open/merged/closed/missing ×
clean/dirty), dry-run output, and that dirty worktrees are never removed.

---

## Out of scope

- GitHub `Unresolved` / `ApprovalsRequired` data gaps (need GraphQL; separate
  spec).
- Any TUI-surface changes beyond the status-footer warning line.
- Auto-pruning worktrees on startup (explicitly rejected in favor of `--gc`).
