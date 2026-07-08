# Quality-Pass Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the weaknesses found in the 2026-07-08 review: formatting/dead code, robustness gaps (regex panic, swallowed state errors, missing GitHub retry), unwired statusline config, expr recompilation, N+1 enrichment, missing GitLab tests, and unbounded worktree growth (`mrglass --gc`).

**Architecture:** Single `fix/quality-pass` branch, one logical commit per task. Every behavioral change is test-driven. New shared code: `internal/provider/execx` (CLI runner + transient retry) and `internal/worktree/gc.go` (GC discovery/classification). Everything else modifies existing files in place.

**Tech Stack:** Go 1.24, stdlib only (no new module dependencies). Existing deps: bubbletea/lipgloss, expr-lang/expr, yaml.v3.

**Spec:** `docs/superpowers/specs/2026-07-08-quality-fixes-design.md`

## Global Constraints

- No new entries in `go.mod`.
- `gofmt -l .` must be empty after every task.
- `go vet ./...` and `go test ./...` must pass after every task.
- Warnings philosophy: config **never errors** — invalid values degrade to defaults with a warning string returned from `normalize()`.
- Writes to a forge (`PostNote`, `pr comment`, `APIPost`) are NEVER retried.
- Work on branch `fix/quality-pass` (create from `main` first: `git checkout -b fix/quality-pass`).

---

### Task 1: Hygiene — gofmt + dead code

**Files:**
- Modify: `cmd/mrglass/main_test.go`, `internal/core/model.go`, `internal/core/model_test.go`, `internal/review/review_test.go`, `internal/tui/app.go`, `internal/tui/app_test.go` (gofmt only)
- Modify: `internal/provider/gitlab/gitlab.go` (delete lines 187-191, 241)
- Modify: `internal/provider/github/github.go` (delete lines 156-157, 308)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing new — pure cleanup. Later tasks assume `parseApprovers` and the `var _ = core.MR{}` lines are gone.

- [ ] **Step 1: Delete dead code in gitlab.go**

Remove these two blocks from `internal/provider/gitlab/gitlab.go`:

```go
// parseApprovers is a thin wrapper kept for backward compatibility.
func parseApprovers(raw []byte) ([]string, error) {
	approvers, _, err := parseApprovals(raw)
	return approvers, err
}
```

and

```go
var _ = core.MR{} // ensure core import used even if signatures change
```

(Keep the `var _ provider.Provider = (*GitLabProvider)(nil)` compile-time check.)

- [ ] **Step 2: Delete dead code in github.go**

In `internal/provider/github/github.go`, the `MRDiff` doc comment is duplicated. Replace:

```go
// MRDiff returns the unified diff for a PR via `gh pr diff`.
//
// MRDiff returns the PR's diff via `gh pr diff`. Implements review.ReviewForge —
// it takes the whole core.MR and parses owner/repo#number from the Ref.
```

with:

```go
// MRDiff returns the PR's diff via `gh pr diff`. Implements review.ReviewForge —
// it takes the whole core.MR and parses owner/repo#number from the Ref.
```

Also remove:

```go
var _ = core.MR{} // ensure core import used even if signatures change
```

- [ ] **Step 3: Format everything**

Run: `gofmt -w cmd/mrglass/main_test.go internal/core/model.go internal/core/model_test.go internal/review/review_test.go internal/tui/app.go internal/tui/app_test.go internal/provider/gitlab/gitlab.go internal/provider/github/github.go`

Then verify: `gofmt -l .`
Expected: no output.

Note: gofmt fixes layout but not the collapsed one-per-line imports in `cmd/mrglass/main_test.go` — it does: `"os";"path/filepath";"testing"` on one line becomes three lines automatically. Verify by reading the file after formatting; imports must be one per line.

- [ ] **Step 4: Verify build and tests**

Run: `go vet ./... && go test ./...`
Expected: all packages `ok`, no vet findings.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: gofmt all files, remove dead code"
```

---

### Task 2: ticketRegex — validate at load, cache compiled regex, never panic

**Files:**
- Modify: `internal/core/model.go` (ParseTicket)
- Modify: `internal/config/config.go` (normalize)
- Test: `internal/core/model_test.go`, `internal/config/config_test.go`

**Interfaces:**
- Consumes: `Default().TicketRegex` (`([A-Z][A-Z0-9]+-\d+)`).
- Produces: `core.ParseTicket(title, branch, pattern string) string` — same signature, but now returns `"Other"` (never panics) on an invalid pattern or a pattern without a capture group, and caches compiled regexes. `config.normalize()` rejects invalid patterns with a warning.

- [ ] **Step 1: Write failing tests for ParseTicket robustness**

Append to `internal/core/model_test.go`:

```go
func TestParseTicketInvalidPatternReturnsOther(t *testing.T) {
	// An unclosed group must not panic.
	if got := ParseTicket("PROJ-1 fix", "b", `([A-Z]+-\d+`); got != "Other" {
		t.Errorf("invalid pattern: got %q, want Other", got)
	}
}

func TestParseTicketPatternWithoutGroupReturnsOther(t *testing.T) {
	// A pattern with no capture group must not panic on m[1].
	if got := ParseTicket("PROJ-1 fix", "b", `[A-Z]+-\d+`); got != "Other" {
		t.Errorf("group-less pattern: got %q, want Other", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/core/ -run TestParseTicket -v`
Expected: FAIL — both new tests panic (`regexp: Compile(...): missing closing )` and `index out of range`).

- [ ] **Step 3: Implement cached, safe ParseTicket**

In `internal/core/model.go`, add `"sync"` to imports and replace `ParseTicket`:

```go
// ticketRes caches compiled ticket patterns; ParseTicket runs per-MR per-refresh
// and the pattern almost never changes.
var ticketRes sync.Map // pattern string -> *regexp.Regexp

func ticketRe(pattern string) (*regexp.Regexp, bool) {
	if v, ok := ticketRes.Load(pattern); ok {
		return v.(*regexp.Regexp), true
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, false
	}
	ticketRes.Store(pattern, re)
	return re, true
}

// ParseTicket extracts a ticket key from the title, then the branch, upper-cased.
// Returns "Other" when neither matches, and also when the pattern is invalid or
// has no capture group (config validation warns about those; this is the
// belt-and-suspenders guard so a bad pattern can never panic the dashboard).
func ParseTicket(title, branch, pattern string) string {
	re, ok := ticketRe(pattern)
	if !ok {
		return "Other"
	}
	for _, s := range []string{title, branch} {
		if m := re.FindStringSubmatch(s); len(m) > 1 {
			return strings.ToUpper(m[1])
		}
	}
	return "Other"
}
```

- [ ] **Step 4: Run core tests**

Run: `go test ./internal/core/ -v`
Expected: PASS (all, including the pre-existing `TestParseTicket` table test).

- [ ] **Step 5: Write failing config validation test**

Append to `internal/config/config_test.go`:

```go
func TestLoadInvalidTicketRegexFallsBackWithWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("ticketRegex: '([A-Z'"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, warns := Load(path)
	if cfg.TicketRegex != Default().TicketRegex {
		t.Errorf("bad regex should fall back to default, got %q", cfg.TicketRegex)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w, "ticketRegex") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a ticketRegex warning, got %v", warns)
	}
}

func TestLoadGrouplessTicketRegexFallsBackWithWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`ticketRegex: '[A-Z]+-\d+'`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, warns := Load(path)
	if cfg.TicketRegex != Default().TicketRegex {
		t.Errorf("group-less regex should fall back to default, got %q", cfg.TicketRegex)
	}
	if len(warns) == 0 {
		t.Error("expected a warning")
	}
}
```

Check the existing import block of `config_test.go`; ensure `"os"`, `"path/filepath"`, `"strings"` are imported (add any missing).

- [ ] **Step 6: Run to verify failure**

Run: `go test ./internal/config/ -run TicketRegex -v`
Expected: FAIL (no fallback happens yet).

- [ ] **Step 7: Validate in normalize()**

In `internal/config/config.go`, add `"regexp"` to imports and add to `normalize()` (after the forge switch, before the jira migration):

```go
	// ticketRegex: must compile and have a capture group (ParseTicket uses m[1]).
	if re, err := regexp.Compile(c.TicketRegex); err != nil {
		warns = append(warns, fmt.Sprintf("invalid ticketRegex %q: %v; using default", c.TicketRegex, err))
		c.TicketRegex = Default().TicketRegex
	} else if re.NumSubexp() < 1 {
		warns = append(warns, fmt.Sprintf("ticketRegex %q has no capture group; using default", c.TicketRegex))
		c.TicketRegex = Default().TicketRegex
	}
```

- [ ] **Step 8: Run tests**

Run: `go test ./internal/config/ ./internal/core/ -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/core/model.go internal/core/model_test.go internal/config/config.go internal/config/config_test.go
git commit -m "fix(config): validate ticketRegex at load; cache compiled pattern, never panic"
```

---

### Task 3: Numeric config validation (Days, RefreshMinutes)

**Files:**
- Modify: `internal/config/config.go` (normalize)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `Default()` values (Days 30, RefreshMinutes 5).
- Produces: after `Load`, `Days >= 1` always; `RefreshMinutes >= 0` always (0 = auto-refresh disabled, an intentional setting).

- [ ] **Step 1: Write failing test**

Append to `internal/config/config_test.go`:

```go
func TestLoadNumericValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("days: -5\nrefreshMinutes: -1"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, warns := Load(path)
	if cfg.Days != 30 {
		t.Errorf("negative days should reset to 30, got %d", cfg.Days)
	}
	if cfg.RefreshMinutes != 0 {
		t.Errorf("negative refreshMinutes should reset to 0, got %d", cfg.RefreshMinutes)
	}
	if len(warns) < 2 {
		t.Errorf("expected warnings for both fields, got %v", warns)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/config/ -run TestLoadNumericValidation -v`
Expected: FAIL.

- [ ] **Step 3: Implement in normalize()**

Add to `normalize()` (after the ticketRegex block from Task 2):

```go
	// Numeric sanity. days <= 0 can't mean anything (0 would show no MRs at
	// all); refreshMinutes: 0 is a real setting (auto-refresh off), negative isn't.
	if c.Days <= 0 {
		warns = append(warns, fmt.Sprintf("days: %d is invalid; using %d", c.Days, Default().Days))
		c.Days = Default().Days
	}
	if c.RefreshMinutes < 0 {
		warns = append(warns, fmt.Sprintf("refreshMinutes: %d is invalid; auto-refresh disabled", c.RefreshMinutes))
		c.RefreshMinutes = 0
	}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/config/ -v`
Expected: PASS. (An explicit `days: 0` in YAML overwrites the default 30 with 0 and is caught by the guard, same as negatives.)

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "fix(config): validate days and refreshMinutes"
```

---

### Task 4: Surface state persistence errors

**Files:**
- Modify: `internal/core/state.go` (LoadState)
- Modify: `internal/watch/watcher.go` (FetchResult, Fetch)
- Modify: `internal/tui/app.go` (fetchResultMsg handler, app.go:389-404)
- Test: `internal/core/state_test.go`, `internal/watch/watcher_test.go`, `internal/tui/app_test.go`

**Interfaces:**
- Consumes: existing `core.SaveState`, `core.LoadState`.
- Produces:
  - `core.LoadState(path string) (map[string]Snapshot, error)` — same signature; a **corrupt** file now returns `(empty non-nil map, error)` so callers can proceed while surfacing the problem. Missing file stays `(empty map, nil)`.
  - `watch.FetchResult` gains `Warning string` (non-fatal state problems).

- [ ] **Step 1: Write failing tests**

Append to `internal/core/state_test.go`:

```go
func TestLoadStateCorruptFileReturnsEmptyMapAndError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadState(path)
	if err == nil {
		t.Error("corrupt state should return an error")
	}
	if m == nil || len(m) != 0 {
		t.Errorf("corrupt state should still return an empty usable map, got %v", m)
	}
}
```

(Check the file's imports include `"os"` and `"path/filepath"`; add if missing.)

Append to `internal/watch/watcher_test.go`:

```go
func TestFetchSurfacesSaveStateFailure(t *testing.T) {
	// A state path whose parent is a FILE makes MkdirAll/WriteFile fail.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(blocker, "state.json")
	res := Fetch(Deps{Provider: stubProvider{}, StatePath: statePath, Cfg: config.Default()})
	if res.Err != nil {
		t.Fatalf("save failure must be non-fatal, got Err=%v", res.Err)
	}
	if res.Warning == "" {
		t.Error("expected a Warning when SaveState fails")
	}
}
```

Look at `internal/watch/watcher_test.go` first: it already has a fake provider used by existing `Fetch` tests — reuse its type name instead of `stubProvider` if one exists (it does; the existing test constructs a provider fake). Match the existing fake's name and construction exactly. Ensure imports include `"os"`, `"path/filepath"`, and `"github.com/whitel1ght/mrglass/internal/config"`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/core/ -run Corrupt -v && go test ./internal/watch/ -run SaveState -v`
Expected: FAIL — LoadState returns nil error on corrupt; `Warning` field doesn't exist (compile error). A compile error in the watch test is the expected "failure" here.

- [ ] **Step 3: Implement LoadState change**

In `internal/core/state.go`, add `"fmt"` to imports, replace the unmarshal branch:

```go
	var m map[string]Snapshot
	if err := json.Unmarshal(b, &m); err != nil {
		// Corrupt file: return a usable empty baseline plus the error, so the
		// caller can proceed as first-run while telling the user their change
		// history was lost.
		return map[string]Snapshot{}, fmt.Errorf("state file %s is corrupt (%v); treating as first run", path, err)
	}
	return m, nil
```

Also update the doc comment on LoadState:

```go
// LoadState reads a {ref: Snapshot} map. A missing file yields an empty map and
// no error (first run). A corrupt file yields an empty map AND an error — the
// caller can proceed as first-run but should surface the warning.
```

- [ ] **Step 4: Implement Fetch change**

In `internal/watch/watcher.go`, update:

```go
type FetchResult struct {
	MRs     []core.MR
	Changes []core.Change
	Warning string // non-fatal problem (state load/save); dashboard still works
	Err     error
}
```

and in `Fetch`:

```go
	prev, loadErr := core.LoadState(d.StatePath)
	// ... (snapshot loop unchanged) ...
	var changes []core.Change
	if len(prev) > 0 {
		changes = core.Diff(prev, curr)
	}
	var warning string
	if loadErr != nil {
		warning = loadErr.Error()
	}
	if err := core.SaveState(d.StatePath, curr); err != nil {
		// Without persisted state every refresh looks like a first run and
		// change detection silently dies — tell the user.
		warning = "state save failed: " + err.Error()
	}
	for _, c := range changes {
		Notify(c)
	}
	return FetchResult{MRs: mrs, Changes: changes, Warning: warning}
```

- [ ] **Step 5: Show the warning in the TUI**

In `internal/tui/app.go` `fetchResultMsg` case (app.go:389-397), after the status line is set:

```go
		m.status = fmt.Sprintf("%d MRs · refreshed %s", len(res.MRs), time.Now().Format("15:04"))
		if res.Warning != "" {
			m.status += "  ⚠ " + res.Warning
		}
```

Add a TUI test in `internal/tui/app_test.go` (mirror the style of existing fetchResultMsg tests — construct the model as neighboring tests do):

```go
func TestFetchWarningShownInStatus(t *testing.T) {
	m := newTestModel(t) // use whatever helper/constructor the neighboring tests use
	m2, _ := m.Update(fetchResultMsg(watch.FetchResult{MRs: nil, Warning: "state save failed: disk full"}))
	m = m2.(Model)
	if !strings.Contains(m.View(), "state save failed") {
		t.Error("fetch warning should appear in the status line")
	}
}
```

Adapt the construction to the actual pattern in `app_test.go` (read its first test; there is no `newTestModel` helper — tests build `Model` via `New(...)` and set `width`/`height`; copy that).

- [ ] **Step 6: Run all affected tests**

Run: `go test ./internal/core/ ./internal/watch/ ./internal/tui/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/core/state.go internal/core/state_test.go internal/watch/watcher.go internal/watch/watcher_test.go internal/tui/app.go internal/tui/app_test.go
git commit -m "fix(watch): surface state load/save failures instead of swallowing them"
```

---

### Task 5: Shared execx package; GitHub gets transient retry

**Files:**
- Create: `internal/provider/execx/execx.go`
- Create: `internal/provider/execx/execx_test.go`
- Modify: `internal/provider/gitlab/glab.go` (delegate to execx)
- Modify: `internal/provider/github/gh.go` (delegate to execx)
- Modify: `internal/provider/github/github.go` (reads retry; writes don't)

**Interfaces:**
- Consumes: nothing (stdlib only).
- Produces:
  ```go
  package execx
  type Runner interface{ Run(args ...string) ([]byte, error) }
  type Exec struct{ Bin string }            // real CLI, 30s timeout, stderr folded into err
  func (Exec) Run(args ...string) ([]byte, error)
  func Retry(r Runner, retries int, args ...string) ([]byte, error) // reads only
  func IsTransient(err error) bool
  var Sleep = time.Sleep                    // test seam
  ```
  `gitlab.Runner` and `github.Runner` become aliases: `type Runner = execx.Runner`.

- [ ] **Step 1: Write failing execx tests**

Create `internal/provider/execx/execx_test.go`:

```go
package execx

import (
	"errors"
	"testing"
	"time"
)

type fakeRunner struct {
	errs  []error // one per call; nil = success
	calls int
}

func (f *fakeRunner) Run(args ...string) ([]byte, error) {
	err := f.errs[f.calls]
	f.calls++
	if err != nil {
		return nil, err
	}
	return []byte("ok"), nil
}

func TestRetryRetriesTransient(t *testing.T) {
	Sleep = func(time.Duration) {}
	defer func() { Sleep = time.Sleep }()
	f := &fakeRunner{errs: []error{errors.New("unexpected EOF"), nil}}
	out, err := Retry(f, 2, "api", "user")
	if err != nil || string(out) != "ok" {
		t.Fatalf("want success after retry, got %q %v", out, err)
	}
	if f.calls != 2 {
		t.Errorf("want 2 calls, got %d", f.calls)
	}
}

func TestRetryStopsOnPermanentError(t *testing.T) {
	Sleep = func(time.Duration) {}
	defer func() { Sleep = time.Sleep }()
	f := &fakeRunner{errs: []error{errors.New("401 unauthorized"), nil}}
	if _, err := Retry(f, 2, "api", "user"); err == nil {
		t.Fatal("permanent error must not be retried into success")
	}
	if f.calls != 1 {
		t.Errorf("want 1 call, got %d", f.calls)
	}
}

func TestRetryGivesUpAfterRetries(t *testing.T) {
	Sleep = func(time.Duration) {}
	defer func() { Sleep = time.Sleep }()
	e := errors.New("connection reset")
	f := &fakeRunner{errs: []error{e, e, e}}
	if _, err := Retry(f, 2, "x"); err == nil {
		t.Fatal("want error after exhausting retries")
	}
	if f.calls != 3 {
		t.Errorf("want 3 calls (1 + 2 retries), got %d", f.calls)
	}
}

func TestIsTransient(t *testing.T) {
	cases := []struct {
		err  string
		want bool
	}{
		{"unexpected EOF", true},
		{"dial tcp: i/o timeout", true},
		{"connection refused", true},
		{"404 not found", false},
	}
	for _, c := range cases {
		if got := IsTransient(errors.New(c.err)); got != c.want {
			t.Errorf("IsTransient(%q) = %v, want %v", c.err, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/provider/execx/ -v`
Expected: FAIL — package doesn't exist / doesn't compile.

- [ ] **Step 3: Implement execx**

Create `internal/provider/execx/execx.go` — this is the retry + exec logic currently living in `internal/provider/gitlab/glab.go` (`ExecRunner.Run`, `APIGet`'s loop, `isTransient`), generalized over the binary name:

```go
// Package execx runs a forge CLI (glab/gh) with a per-call timeout, folding
// stderr into errors, and retries transient transport failures for reads.
// Writes must never go through Retry — a transient-looking failure could have
// actually succeeded, and we never want a duplicate comment.
package execx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Runner executes a CLI. Abstracted so tests can fake it (no network).
type Runner interface {
	Run(args ...string) ([]byte, error)
}

// Exec runs the real binary with a per-call timeout.
type Exec struct{ Bin string }

func (e Exec) Run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.Bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// The CLI puts the real failure (auth, 4xx, validation) on stderr;
		// without this the caller only sees an opaque "exit status N".
		if s := strings.TrimSpace(stderr.String()); s != "" {
			err = fmt.Errorf("%v: %s", err, s)
		}
	}
	return stdout.Bytes(), err
}

// Sleep is time.Sleep, overridable in tests to avoid real backoff waits.
var Sleep = time.Sleep

// Retry runs r.Run(args...), retrying transient transport failures with linear
// backoff. READS ONLY — never route a write through this.
func Retry(r Runner, retries int, args ...string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		out, err := r.Run(args...)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if !IsTransient(err) {
			break
		}
		Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
	}
	return nil, lastErr
}

// IsTransient classifies an error as a retryable transport failure.
func IsTransient(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "eof") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "connection")
}
```

Run: `go test ./internal/provider/execx/ -v`
Expected: PASS.

- [ ] **Step 4: Delegate glab.go to execx**

Replace the entire contents of `internal/provider/gitlab/glab.go` with:

```go
package gitlab

import (
	"github.com/whitel1ght/mrglass/internal/provider/execx"
)

// Runner executes the glab CLI. Alias of the shared execx.Runner so tests can
// fake it.
type Runner = execx.Runner

// NewRunner returns a Runner for the real glab binary (30s per-call timeout).
func NewRunner() Runner { return execx.Exec{Bin: "glab"} }

// APIGet runs `glab api <path>`, retrying transient transport failures.
func APIGet(r Runner, path string, retries int) ([]byte, error) {
	return execx.Retry(r, retries, "api", path)
}

// APIPost runs `glab api -X POST <path>` with the given form fields. Used for
// the few write operations mrglass performs (e.g. posting an MR note). Writes
// are NOT retried — a transient-looking failure could have actually succeeded,
// and we never want to post a duplicate comment.
func APIPost(r Runner, path string, fields map[string]string) ([]byte, error) {
	args := []string{"api", "-X", "POST", path}
	for k, v := range fields {
		args = append(args, "-f", k+"="+v)
	}
	return r.Run(args...)
}
```

Update `internal/provider/gitlab/gitlab.go` line 19: `func New() *GitLabProvider { return &GitLabProvider{R: NewRunner()} }`

Check `internal/provider/gitlab/glab_test.go` — it tests `isTransient`/retry behavior against the old local functions; move/adapt those tests: delete ones now covered by `execx_test.go`, keep any `APIPost` arg-shape tests (they still compile against the new file).

- [ ] **Step 5: Delegate gh.go to execx and add read retry**

Replace the contents of `internal/provider/github/gh.go` with:

```go
package github

import (
	"github.com/whitel1ght/mrglass/internal/provider/execx"
)

// Runner executes the gh CLI. Alias of the shared execx.Runner so tests can
// fake it (no network).
type Runner = execx.Runner

// NewRunner runs the real gh binary with a per-call timeout.
func NewRunner() Runner { return execx.Exec{Bin: "gh"} }

// run executes a READ command, retrying transient transport failures (parity
// with the GitLab provider). Writes (pr comment) call r.Run directly.
func run(r Runner, args ...string) ([]byte, error) {
	return execx.Retry(r, 2, args...)
}
```

In `internal/provider/github/github.go`:
- line 21: `func New() *GitHubProvider { return &GitHubProvider{R: NewRunner()} }`
- `PostNote` must NOT retry — change its call from `run(p.R, ...)` to `p.R.Run(...)`:

```go
	_, err := p.R.Run("pr", "comment", strconv.Itoa(number), "--repo", repo, "--body", body)
```

(`Whoami`, `List`, `enrich`, `MRDiff` keep using `run` and therefore now retry.)

- [ ] **Step 6: Write a GitHub retry test**

Append to `internal/provider/github/github_test.go` (its `fakeRunner` returns canned bytes per subcommand — add a transient-then-success variant):

```go
type flakyRunner struct {
	fails int // fail this many calls with a transient error, then succeed
	inner Runner
	calls int
}

func (f *flakyRunner) Run(args ...string) ([]byte, error) {
	f.calls++
	if f.calls <= f.fails {
		return nil, errors.New("unexpected EOF")
	}
	return f.inner.Run(args...)
}

func TestWhoamiRetriesTransient(t *testing.T) {
	execx.Sleep = func(time.Duration) {}
	defer func() { execx.Sleep = time.Sleep }()
	inner := newFakeRunner(t) // adapt: construct the existing fakeRunner exactly as TestWhoami does
	p := &GitHubProvider{R: &flakyRunner{fails: 1, inner: inner}}
	me, err := p.Whoami()
	if err != nil {
		t.Fatalf("want retry success, got %v", err)
	}
	if me == "" {
		t.Error("want a username after retry")
	}
}
```

Adapt `newFakeRunner(t)` to however the existing `TestWhoami` builds its fake (read `github_test.go:17-39` and copy). Add imports `"errors"`, `"time"`, and `"github.com/whitel1ght/mrglass/internal/provider/execx"`.

- [ ] **Step 7: Run everything**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all PASS. If `glab_test.go` or `github_test.go` referenced `ExecRunner` directly, replace with `NewRunner()` or `execx.Exec{Bin: ...}`.

- [ ] **Step 8: Commit**

```bash
git add internal/provider/
git commit -m "refactor(provider): shared execx runner; GitHub reads retry transient failures"
```

---

### Task 6: Statusline — remove Align, wire per-segment Style

**Files:**
- Modify: `internal/config/config.go` (Segment: drop Align; Default: drop `Align: "right"`)
- Modify: `internal/tui/statusline/statusline.go` (configStyle, namedStyle)
- Modify: `config/config.example.yaml` (line 103: drop `align: right`)
- Test: `internal/tui/statusline/statusline_test.go`

**Interfaces:**
- Consumes: `theme.Styles` fields (Base, Subtle, Accent, Success, Warn, Danger, Advice).
- Produces: `Segment.Style` names resolve: `base | subtle | faint | accent | success | warn | danger | advice` (`faint` = Subtle + lipgloss Faint). Unknown name → semantic default kept. Precedence: `Styles` map (ci) → `Style` name → semantic default. `Segment.Align` no longer exists.

- [ ] **Step 1: Write failing tests**

Append to `internal/tui/statusline/statusline_test.go` (mirror existing tests' setup for `theme.Styles`/`RowView` — read the file's first test and reuse its helpers):

```go
func TestSegmentStyleNameHonored(t *testing.T) {
	st := theme.BuildStyles(theme.Get("tokyonight"))
	seg := config.Segment{Type: "text", Source: "title", Style: "danger"}
	rv := RowView{MR: core.MR{Title: "hello"}}
	got := renderSegment(seg, st, rv, st.Base)
	want := st.Danger.Inline(true).Render("hello")
	if got != want {
		t.Errorf("style name not honored:\n got %q\nwant %q", got, want)
	}
}

func TestSegmentUnknownStyleNameKeepsDefault(t *testing.T) {
	st := theme.BuildStyles(theme.Get("tokyonight"))
	seg := config.Segment{Type: "text", Source: "title", Style: "nope"}
	rv := RowView{MR: core.MR{Title: "hello"}}
	got := renderSegment(seg, st, rv, st.Base)
	want := st.Base.Inline(true).Render("hello")
	if got != want {
		t.Errorf("unknown style should keep default:\n got %q\nwant %q", got, want)
	}
}
```

Note: `renderSegment` gains a 4th param `base lipgloss.Style` in Task 8; to avoid churn, add it NOW (this task) — signature `renderSegment(s config.Segment, st theme.Styles, rv RowView, base lipgloss.Style) string`, with `base` used where `st.Base` was. Task 8 then only changes what callers pass.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/statusline/ -run SegmentStyle -v`
Expected: FAIL (compile error — wrong arity — or wrong style).

- [ ] **Step 3: Implement**

In `internal/tui/statusline/statusline.go`:

1. `renderSegment` signature: `func renderSegment(s config.Segment, st theme.Styles, rv RowView, base lipgloss.Style) string`. Replace `style := st.Base` with `style := base`, and in the `"text"` and `"comments"` cases replace `st.Base` with `base`. Update the call in `renderGroup`: `renderSegment(s, st, rv, base)` — `renderGroup` also gains the param: `func renderGroup(segs []config.Segment, st theme.Styles, rv RowView, env map[string]any, base lipgloss.Style) string`. In `Render`, call `renderGroup(cfg.Left, st, rv, env, st.Base)` (and same for Right).
2. Replace `configStyle`:

```go
// configStyle returns an explicit per-segment style from config, if any.
// Precedence: per-value Styles map (ci, keyed by status) → named Style →
// none. Unknown style names are ignored (semantic default kept) — config
// mistakes must never break rendering.
func configStyle(s config.Segment, st theme.Styles, mr core.MR) (lipgloss.Style, bool) {
	if s.Type == "ci" && len(s.Styles) > 0 {
		if sc, ok := s.Styles[mr.CI]; ok {
			return theme.StyleFrom(sc), true
		}
	}
	if s.Style != "" {
		return namedStyle(st, s.Style)
	}
	return lipgloss.Style{}, false
}

// namedStyle resolves a config style name to a theme style.
func namedStyle(st theme.Styles, name string) (lipgloss.Style, bool) {
	switch name {
	case "base":
		return st.Base, true
	case "subtle":
		return st.Subtle, true
	case "faint":
		return st.Subtle.Faint(true), true
	case "accent":
		return st.Accent, true
	case "success":
		return st.Success, true
	case "warn":
		return st.Warn, true
	case "danger":
		return st.Danger, true
	case "advice":
		return st.Advice, true
	}
	return lipgloss.Style{}, false
}
```

Update the `configStyle` call site in `renderSegment`: `if override, ok := configStyle(s, st, mr); ok {`.

3. In `internal/config/config.go`: delete the `Align string \`yaml:"align"\`` field from `Segment`, and delete `Align: "right",` from the default age segment (config.go:161). YAML strict mode is not used, so old configs with `align:` load fine (ignored).
4. In `config/config.example.yaml` line 103: change `- { type: age, source: updatedAt, align: right, style: faint }` to `- { type: age, source: updatedAt, style: faint }`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/... ./internal/config/ -v`
Expected: PASS (fix any test that referenced `Align`).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/tui/statusline/ config/config.example.yaml
git commit -m "feat(statusline): honor per-segment style names; drop redundant align field"
```

---

### Task 7: Statusline — wire Grow (row never exceeds width)

**Files:**
- Modify: `internal/tui/statusline/statusline.go` (Render, truncate helper)
- Test: `internal/tui/statusline/statusline_test.go`

**Interfaces:**
- Consumes: Task 6's `renderGroup(segs, st, rv, env, base)`.
- Produces: `Render(...)` output whose `lipgloss.Width` never exceeds `width` when a `Grow: true` **text** segment exists (grow shrinks to a 4-rune floor; beyond that overflow can still happen and is acceptable). The default config's title segment has `Grow: true`.

- [ ] **Step 1: Write failing test**

```go
func TestGrowSegmentAbsorbsOverflow(t *testing.T) {
	st := theme.BuildStyles(theme.Get("tokyonight"))
	cfg := config.StatuslineConfig{
		Left:  []config.Segment{{Type: "text", Source: "title", Grow: true, MaxWidth: 60}},
		Right: []config.Segment{{Type: "age"}},
	}
	rv := RowView{MR: core.MR{
		Title:     strings.Repeat("long title ", 10),
		UpdatedAt: time.Now().Add(-2 * time.Hour),
	}}
	const width = 40
	line := Render(cfg, st, rv, width, false)
	if w := lipgloss.Width(line); w > width {
		t.Errorf("row width %d exceeds terminal width %d", w, width)
	}
	if !strings.Contains(line, "…") {
		t.Error("grow segment should be truncated with an ellipsis")
	}
}
```

Add imports as needed (`"strings"`, `"time"`, `"github.com/charmbracelet/lipgloss"`).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/statusline/ -run Grow -v`
Expected: FAIL — width exceeds 40 (gap clamps to 1 and overflows today).

- [ ] **Step 3: Implement**

In `internal/tui/statusline/statusline.go`:

1. Extract the truncation from the `"text"` case into a helper and use it there:

```go
// truncate cuts s to max runes, ending with … when cut. max <= 0 means no limit.
func truncate(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}
```

The `"text"` case becomes:

```go
	case "text":
		text = truncate(fieldString(s.Source, mr), s.MaxWidth)
		style = base
```

2. In `Render`, after computing `left`/`right`, shrink the first `Grow` segment when the row overflows:

```go
	left := renderGroup(cfg.Left, st, rv, env, st.Base)
	right := renderGroup(cfg.Right, st, rv, env, st.Base)

	// A grow segment absorbs width pressure: when the row would overflow the
	// terminal, re-render its group with the grow segment shrunk by the
	// overflow (floor 4 runes) so the row fits instead of wrapping.
	if over := lipgloss.Width(left) + 1 + lipgloss.Width(right) - width; over > 0 {
		if segs, i := findGrow(cfg.Left); i >= 0 {
			left = renderShrunk(segs, i, over, st, rv, env, st.Base)
		} else if segs, i := findGrow(cfg.Right); i >= 0 {
			right = renderShrunk(segs, i, over, st, rv, env, st.Base)
		}
	}

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	// ... rest unchanged
```

3. Add the helpers:

```go
// findGrow returns the segments and the index of the first Grow segment, or -1.
func findGrow(segs []config.Segment) ([]config.Segment, int) {
	for i, s := range segs {
		if s.Grow {
			return segs, i
		}
	}
	return nil, -1
}

// renderShrunk re-renders a group with segment i's MaxWidth reduced by over.
func renderShrunk(segs []config.Segment, i, over int, st theme.Styles, rv RowView, env map[string]any, base lipgloss.Style) string {
	s := segs[i]
	cur := lipgloss.Width(renderSegment(s, st, rv, base))
	newMax := cur - over
	if newMax < 4 {
		newMax = 4
	}
	s.MaxWidth = newMax
	out := make([]config.Segment, len(segs))
	copy(out, segs)
	out[i] = s
	return renderGroup(out, st, rv, env, base)
}
```

(Grow only affects `text` segments — MaxWidth is only honored there; a grow flag on another type is a harmless no-op. Note this in `config/config.example.yaml` if not already implied.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/... -v`
Expected: PASS, including pre-existing width/layout tests in `app_test.go` (`go test ./internal/tui/ -v`).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/statusline/
git commit -m "feat(statusline): grow segment absorbs overflow so rows fit the terminal"
```

---

### Task 8: Statusline — wire States (selected, ci_failed)

**Files:**
- Modify: `internal/tui/statusline/statusline.go` (Render)
- Test: `internal/tui/statusline/statusline_test.go`
- Modify: `config/config.example.yaml` (document the two supported keys)

**Interfaces:**
- Consumes: Task 6's `renderGroup(..., base lipgloss.Style)`; `theme.StyleFrom`.
- Produces: `States["ci_failed"]` replaces the row's **base** style (title/comments text) when `mr.CI == "failed"`; `States["selected"]` replaces the theme's `st.Selected` wrap for the cursor row. Only these two keys are read.

- [ ] **Step 1: Write failing tests**

```go
func TestStatesCIFailedRestylesBaseText(t *testing.T) {
	st := theme.BuildStyles(theme.Get("tokyonight"))
	cfg := config.StatuslineConfig{
		States: map[string]config.StyleConfig{"ci_failed": {FG: "#ff0000"}},
		Left:   []config.Segment{{Type: "text", Source: "title"}},
	}
	rv := RowView{MR: core.MR{Title: "boom", CI: "failed"}}
	line := Render(cfg, st, rv, 80, false)
	want := theme.StyleFrom(config.StyleConfig{FG: "#ff0000"}).Inline(true).Render("boom")
	if !strings.Contains(line, want) {
		t.Errorf("ci_failed state should restyle base text\n got: %q\nwant substring: %q", line, want)
	}
}

func TestStatesSelectedOverridesThemeBar(t *testing.T) {
	st := theme.BuildStyles(theme.Get("tokyonight"))
	cfg := config.StatuslineConfig{
		States: map[string]config.StyleConfig{"selected": {BG: "#123456"}},
		Left:   []config.Segment{{Type: "text", Source: "title"}},
	}
	rv := RowView{MR: core.MR{Title: "row"}}
	line := Render(cfg, st, rv, 80, true)
	if !strings.Contains(line, "#123456") && !strings.Contains(line, "18;52;86") {
		// lipgloss renders hex as 24-bit "38/48;2;R;G;B"; accept either encoding.
		t.Errorf("selected state style not applied: %q", line)
	}
}
```

(If the second assertion proves brittle against lipgloss's actual escape encoding, assert instead that `Render(..., true)` output differs between `States` set and unset — behavioral, encoding-agnostic. Prefer that if the first form fails for encoding reasons.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/statusline/ -run States -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

In `Render` (`internal/tui/statusline/statusline.go`):

```go
func Render(cfg config.StatuslineConfig, st theme.Styles, rv RowView, width int, selected bool) string {
	env := exprEnv(rv)

	// Row-state overrides from config. "ci_failed" replaces the BASE style
	// (plain text like the title) so the whole row reads as failed; semantic
	// segment colors (the ci symbol itself, approvals) are unaffected.
	base := st.Base
	if rv.MR.CI == "failed" {
		if sc, ok := cfg.States["ci_failed"]; ok {
			base = theme.StyleFrom(sc)
		}
	}

	left := renderGroup(cfg.Left, st, rv, env, base)
	right := renderGroup(cfg.Right, st, rv, env, base)
	// ... grow logic from Task 7, passing `base` instead of st.Base ...

	line := left + strings.Repeat(" ", gap) + right
	if selected {
		// "selected" replaces the theme's selection bar when configured.
		sel := st.Selected
		if sc, ok := cfg.States["selected"]; ok {
			sel = theme.StyleFrom(sc)
		}
		return sel.Render(line)
	}
	return line
}
```

(Also update the two `renderShrunk`/`renderGroup` call sites from Task 7 to pass `base`.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/... -v`
Expected: PASS.

- [ ] **Step 5: Document in the example config**

In `config/config.example.yaml`, above the `states:` block (line 87), add:

```yaml
  # Row-state style overrides. Supported keys:
  #   selected  — replaces the theme's cursor-row bar
  #   ci_failed — restyles the row's plain text (title etc.) when CI is failed
```

- [ ] **Step 6: Commit**

```bash
git add internal/tui/statusline/ config/config.example.yaml
git commit -m "feat(statusline): honor states.selected and states.ci_failed row overrides"
```

---

### Task 9: Cache compiled expr programs; fix section filter approvals bug

**Files:**
- Modify: `internal/tui/section/section.go` (Match signature, cache)
- Modify: `internal/tui/statusline/statusline.go` (evalBool cache)
- Modify: `internal/tui/section/section_test.go` (Match arity)
- Test: `internal/tui/section/section_test.go`

**Interfaces:**
- Consumes: `expr` + `github.com/expr-lang/expr/vm` (already in the module via expr-lang).
- Produces: `section.Match(filter string, mr core.MR) bool` — **third param dropped**; `required` in filter expressions now sees `mr.ApprovalsRequired` (was hardcoded 0). `section.Filter` unchanged signature. Compiled programs cached per expression string.

- [ ] **Step 1: Write failing test for the approvals bug**

In `internal/tui/section/section_test.go`, update existing `Match` calls to the new 2-arg form (`Match(\`role == "mine"\`, mine)` etc. — they won't compile until Step 3, which is the failing state) and add:

```go
func TestMatchSeesRealApprovalsRequired(t *testing.T) {
	mr := core.MR{ApprovalsRequired: 2}
	if !Match(`required == 2`, mr) {
		t.Error("filter should see the MR's real ApprovalsRequired")
	}
	if Match(`required == 0`, mr) {
		t.Error("required must not be hardcoded to 0")
	}
}

func TestFilterUsesPerMRApprovals(t *testing.T) {
	mrs := []core.MR{{Ref: "a", ApprovalsRequired: 2}, {Ref: "b", ApprovalsRequired: 0}}
	got := Filter(`required > 0`, mrs)
	if len(got) != 1 || got[0].Ref != "a" {
		t.Errorf("Filter should evaluate required per MR, got %v", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/section/ -v`
Expected: FAIL (compile error on arity — expected; that's the red state).

- [ ] **Step 3: Implement**

Replace `internal/tui/section/section.go`'s env/Match/Filter:

```go
import (
	"sort"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/whitel1ght/mrglass/internal/core"
)

func env(mr core.MR) map[string]any {
	return map[string]any{
		"role":       mr.Role.String(),
		"ci":         mr.CI,
		"draft":      mr.Draft,
		"conflicts":  mr.Conflicts,
		"unresolved": mr.Unresolved,
		"comments":   mr.Comments,
		"approvedBy": mr.ApprovedBy,
		"required":   mr.ApprovalsRequired,
		"author":     mr.Author,
		"title":      mr.Title,
	}
}

// progs caches compiled filter programs; filters are fixed config strings
// evaluated per MR per render, so compile each once.
var progs sync.Map // filter string -> *vm.Program

func compile(filter string, e map[string]any) (*vm.Program, error) {
	if v, ok := progs.Load(filter); ok {
		return v.(*vm.Program), nil
	}
	prog, err := expr.Compile(filter, expr.Env(e), expr.AsBool())
	if err != nil {
		return nil, err
	}
	progs.Store(filter, prog)
	return prog, nil
}

// Match evaluates a filter predicate against an MR. A broken filter matches nothing.
func Match(filter string, mr core.MR) bool {
	e := env(mr)
	prog, err := compile(filter, e)
	if err != nil {
		return false
	}
	out, err := expr.Run(prog, e)
	if err != nil {
		return false
	}
	b, _ := out.(bool)
	return b
}

// Filter returns the MRs matching the predicate, preserving order.
func Filter(filter string, mrs []core.MR) []core.MR {
	var out []core.MR
	for _, mr := range mrs {
		if Match(filter, mr) {
			out = append(out, mr)
		}
	}
	return out
}
```

Same caching in `internal/tui/statusline/statusline.go` — replace `evalBool`:

```go
// whenProgs caches compiled `when` predicates (fixed config strings).
var whenProgs sync.Map // code string -> *vm.Program

func evalBool(code string, env map[string]any) bool {
	var prog *vm.Program
	if v, ok := whenProgs.Load(code); ok {
		prog = v.(*vm.Program)
	} else {
		p, err := expr.Compile(code, expr.Env(env), expr.AsBool())
		if err != nil {
			return false
		}
		whenProgs.Store(code, p)
		prog = p
	}
	out, err := expr.Run(prog, env)
	if err != nil {
		return false
	}
	b, _ := out.(bool)
	return b
}
```

Add imports `"sync"` and `"github.com/expr-lang/expr/vm"` to statusline.go.

- [ ] **Step 4: Run tests (whole TUI — app.go uses section.Filter)**

Run: `go build ./... && go test ./internal/tui/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/section/ internal/tui/statusline/
git commit -m "perf(tui): cache compiled expr programs; fix section filters seeing required=0"
```

---

### Task 10: Concurrent per-MR enrichment via shared EnrichAll

**Files:**
- Create: `internal/provider/enrich.go`
- Create: `internal/provider/enrich_test.go`
- Modify: `internal/provider/gitlab/gitlab.go` (List)
- Modify: `internal/provider/github/github.go` (List)
- Modify: `internal/provider/github/github_test.go` (fakeRunner mutex)

**Interfaces:**
- Consumes: existing per-provider `enrich` methods (unchanged).
- Produces: `provider.EnrichAll(found map[string]core.MR, limit int, enrich func(core.MR) core.MR) []core.MR` — runs enrich concurrently with at most `limit` in flight and returns the enriched MRs (order unspecified, matching today's map iteration). `List` behavior otherwise unchanged. Fakes used with these providers MUST be goroutine-safe.

- [ ] **Step 1: Write failing EnrichAll tests**

Create `internal/provider/enrich_test.go`:

```go
package provider

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/whitel1ght/mrglass/internal/core"
)

func TestEnrichAllEnrichesEveryMR(t *testing.T) {
	found := map[string]core.MR{
		"a": {Ref: "a"}, "b": {Ref: "b"}, "c": {Ref: "c"},
	}
	var mu sync.Mutex
	seen := map[string]bool{}
	out := EnrichAll(found, 4, func(mr core.MR) core.MR {
		mu.Lock()
		seen[mr.Ref] = true
		mu.Unlock()
		mr.Title = "enriched"
		return mr
	})
	if len(out) != 3 || len(seen) != 3 {
		t.Fatalf("want all 3 enriched, got out=%d seen=%d", len(out), len(seen))
	}
	for _, mr := range out {
		if mr.Title != "enriched" {
			t.Errorf("%s: enrich result not kept", mr.Ref)
		}
	}
}

func TestEnrichAllBoundsConcurrency(t *testing.T) {
	found := map[string]core.MR{}
	for _, r := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		found[r] = core.MR{Ref: r}
	}
	var inFlight, peak atomic.Int32
	gate := make(chan struct{})
	out := make(chan []core.MR, 1)
	go func() {
		out <- EnrichAll(found, 2, func(mr core.MR) core.MR {
			n := inFlight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			<-gate
			inFlight.Add(-1)
			return mr
		})
	}()
	close(gate) // release everyone; peak was recorded on entry
	if got := <-out; len(got) != 8 {
		t.Fatalf("want 8 results, got %d", len(got))
	}
	if p := peak.Load(); p > 2 {
		t.Errorf("concurrency peaked at %d, limit was 2", p)
	}
}

func TestEnrichAllEmpty(t *testing.T) {
	if out := EnrichAll(nil, 4, func(mr core.MR) core.MR { return mr }); len(out) != 0 {
		t.Errorf("nil input should give empty output, got %v", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/provider/ -v`
Expected: FAIL (undefined: EnrichAll).

- [ ] **Step 3: Implement EnrichAll**

Create `internal/provider/enrich.go`:

```go
package provider

import (
	"sync"

	"github.com/whitel1ght/mrglass/internal/core"
)

// EnrichAll runs enrich over every MR with at most limit calls in flight —
// the per-MR detail fetch is each provider's slow path, and 3-4 concurrent
// calls stays polite to the forge API. Result order is unspecified (callers
// previously iterated a map). Each result index is written by exactly one
// goroutine.
func EnrichAll(found map[string]core.MR, limit int, enrich func(core.MR) core.MR) []core.MR {
	result := make([]core.MR, len(found))
	var wg sync.WaitGroup
	sem := make(chan struct{}, limit)
	i := 0
	for _, mr := range found {
		wg.Add(1)
		go func(idx int, mr core.MR) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			result[idx] = enrich(mr)
		}(i, mr)
		i++
	}
	wg.Wait()
	return result
}
```

Run: `go test -race ./internal/provider/ -v`
Expected: PASS.

- [ ] **Step 4: Make the GitHub fakeRunner goroutine-safe**

In `internal/provider/github/github_test.go`, the `fakeRunner` records calls; guard it (adapt names to the actual struct at github_test.go:17-39):

```go
type fakeRunner struct {
	mu sync.Mutex
	// ... existing fields ...
}

func (f *fakeRunner) Run(args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// ... existing body ...
}
```

Add `"sync"` to imports. Task 11's gitlab `fakeRunner` is already mutex-guarded.

- [ ] **Step 5: Wire both List functions**

In `internal/provider/gitlab/gitlab.go`, replace:

```go
	result := make([]core.MR, 0, len(found))
	for _, mr := range found {
		mr = p.enrich(mr)
		result = append(result, mr)
	}
	return result, nil
```

with:

```go
	return provider.EnrichAll(found, 4, p.enrich), nil
```

(`provider` is already imported for the compile-time check.)

In `internal/provider/github/github.go`, same replacement with a closure over the extra args:

```go
	return provider.EnrichAll(found, 4, func(mr core.MR) core.MR {
		return p.enrich(mr, me, ticketPattern)
	}), nil
```

- [ ] **Step 6: Run with the race detector**

Run: `go test -race ./internal/provider/... -v`
Expected: PASS, no race reports. The existing `TestListThreeBucketsAndDedupe` and `TestEnrichCarriesApprovalsFailure` cover behavior; `-race` covers the new concurrency.

- [ ] **Step 7: Run full suite**

Run: `go test -race ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/provider/
git commit -m "perf(provider): enrich MRs concurrently via shared EnrichAll (bounded)"
```

---

### Task 11: GitLab provider test parity

**Files:**
- Modify: `internal/provider/gitlab/gitlab_test.go` (add fakeRunner + exec-path tests)
- Existing fixtures: `internal/provider/gitlab/testdata/mrs.json`, `testdata/approvals.json`

**Interfaces:**
- Consumes: `gitlab.Runner` (= `execx.Runner`), fixtures, Task 10's concurrent List.
- Produces: nothing new — coverage. Target ≥ 75% for the gitlab package.

- [ ] **Step 1: Add a goroutine-safe fakeRunner**

Append to `internal/provider/gitlab/gitlab_test.go` (mirroring `github_test.go`'s design):

```go
// fakeRunner returns canned bytes keyed by a substring of the joined args, and
// records every call. Goroutine-safe (List enriches concurrently).
type fakeRunner struct {
	mu        sync.Mutex
	responses map[string][]byte // substring of strings.Join(args, " ") -> payload
	errFor    string            // args containing this substring return an error
	calls     []string
}

func (f *fakeRunner) Run(args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	joined := strings.Join(args, " ")
	f.calls = append(f.calls, joined)
	if f.errFor != "" && strings.Contains(joined, f.errFor) {
		return nil, errors.New("boom: " + joined)
	}
	for k, v := range f.responses {
		if strings.Contains(joined, k) {
			return v, nil
		}
	}
	return []byte(`[]`), nil
}
```

Add imports: `"errors"`, `"strings"`, `"sync"`, `"os"` (for fixtures).

- [ ] **Step 2: Write the tests**

```go
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWhoami(t *testing.T) {
	f := &fakeRunner{responses: map[string][]byte{"api user": []byte(`{"username":"dmitry"}`)}}
	p := &GitLabProvider{R: f}
	me, err := p.Whoami()
	if err != nil || me != "dmitry" {
		t.Fatalf("got %q, %v", me, err)
	}
}

func TestListThreeBucketsDedupeAndEnrich(t *testing.T) {
	f := &fakeRunner{responses: map[string][]byte{
		"scope=created_by_me": fixture(t, "mrs.json"),
		"scope=assigned_to_me": fixture(t, "mrs.json"), // same MRs → must dedupe
		"reviewer_username":    []byte(`[]`),
		"/approvals":           fixture(t, "approvals.json"),
	}}
	p := &GitLabProvider{R: f}
	mrs, err := p.List("dmitry", 100000, `([A-Z][A-Z0-9]+-\d+)`)
	if err != nil {
		t.Fatal(err)
	}
	// mrs.json fixture count — adjust to the fixture's actual length:
	var raw []map[string]any
	_ = json.Unmarshal(fixture(t, "mrs.json"), &raw)
	if len(mrs) != len(raw) {
		t.Errorf("dedupe failed: got %d MRs, fixture has %d", len(mrs), len(raw))
	}
	for _, mr := range mrs {
		if !mr.ApprovalsOK() {
			t.Errorf("%s: enrich should have succeeded", mr.Ref)
		}
	}
}

func TestListEnrichFailureIsNonFatal(t *testing.T) {
	f := &fakeRunner{
		responses: map[string][]byte{"scope=created_by_me": fixture(t, "mrs.json")},
		errFor:    "/approvals",
	}
	p := &GitLabProvider{R: f}
	mrs, err := p.List("dmitry", 100000, `([A-Z][A-Z0-9]+-\d+)`)
	if err != nil {
		t.Fatal("enrich failure must not fail List")
	}
	for _, mr := range mrs {
		if mr.ApprovalsOK() {
			t.Errorf("%s: ApprovalsOK should be false after enrich failure", mr.Ref)
		}
	}
}

func TestListBucketFetchFailureIsFatal(t *testing.T) {
	f := &fakeRunner{errFor: "scope=created_by_me"}
	p := &GitLabProvider{R: f}
	if _, err := p.List("dmitry", 30, `([A-Z][A-Z0-9]+-\d+)`); err == nil {
		t.Error("bucket fetch failure should fail List")
	}
}

func TestMRDiffConcatenatesChanges(t *testing.T) {
	payload := []byte(`{"changes":[
		{"old_path":"a.go","new_path":"a.go","diff":"@@ -1 +1 @@\n-x\n+y"},
		{"old_path":"gone.go","new_path":"","diff":"@@ deleted @@"}]}`)
	f := &fakeRunner{responses: map[string][]byte{"/changes": payload}}
	p := &GitLabProvider{R: f}
	diff, err := p.MRDiff(core.MR{ProjectID: 7, IID: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "--- a.go\n") || !strings.Contains(diff, "--- gone.go\n") {
		t.Errorf("diff missing file headers (deleted file should use old_path):\n%s", diff)
	}
}

func TestPostNoteArgs(t *testing.T) {
	f := &fakeRunner{}
	p := &GitLabProvider{R: f}
	if err := p.PostNote(core.MR{ProjectID: 7, IID: 3}, "hello"); err != nil {
		t.Fatal(err)
	}
	want := "api -X POST projects/7/merge_requests/3/notes -f body=hello"
	if len(f.calls) != 1 || f.calls[0] != want {
		t.Errorf("got calls %v, want [%s]", f.calls, want)
	}
}
```

Adjust `TestListThreeBucketsDedupeAndEnrich`'s expected count after reading `testdata/mrs.json` (open it; if the response-key substrings collide with the fixture's URL contents, pick more specific keys). Add `"encoding/json"` import.

- [ ] **Step 3: Run and check coverage**

Run: `go test -race -cover ./internal/provider/gitlab/ -v`
Expected: PASS, coverage ≥ 75% (was 47.4%).

- [ ] **Step 4: Commit**

```bash
git add internal/provider/gitlab/
git commit -m "test(gitlab): exec-path coverage parity with the GitHub suite"
```

---

### Task 12: Worktree GC — discovery, classification, removal

**Files:**
- Create: `internal/worktree/gc.go`
- Create: `internal/worktree/gc_test.go`

**Interfaces:**
- Consumes: `worktree.GitRunner` (existing), `worktree.Slug` (existing).
- Produces:
  ```go
  type GCItem struct {
      RepoDir string // main clone the worktree belongs to
      Path    string // worktree directory
      Branch  string // "mrglass/<slug>"
      Slug    string
      Dirty   bool   // uncommitted changes or unpushed commits
  }
  func DefaultBase(repoDir string) string
  func ListGC(g GitRunner, repoDir, baseDir string) ([]GCItem, error)
  func Removable(items []GCItem, openSlugs map[string]bool) (remove, skipDirty []GCItem)
  func (g Git) Remove(item GCItem) error
  ```

- [ ] **Step 1: Write failing tests**

Create `internal/worktree/gc_test.go`:

```go
package worktree

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// gcGit fakes GitRunner keyed by the joined args (worktree_test.go has a
// similar fake for Prepare; this one also varies by repoDir/dir).
type gcGit struct {
	out   map[string]string // "<dir>|<args joined>" -> stdout
	calls []string
}

func (g *gcGit) Run(dir string, args ...string) ([]byte, error) {
	key := dir + "|" + strings.Join(args, " ")
	g.calls = append(g.calls, key)
	if v, ok := g.out[key]; ok {
		return []byte(v), nil
	}
	return nil, errors.New("unexpected git call: " + key)
}

func TestListGCFindsMrglassWorktrees(t *testing.T) {
	repo := "/p/api"
	base := DefaultBase(repo) // /p/.mrglass-worktrees
	wt := filepath.Join(base, "api-PROJ-1")
	porcelain := "worktree " + repo + "\nHEAD aaa\nbranch refs/heads/main\n\n" +
		"worktree " + wt + "\nHEAD bbb\nbranch refs/heads/mrglass/api-PROJ-1\n\n"
	g := &gcGit{out: map[string]string{
		repo + "|worktree list --porcelain": porcelain,
		wt + "|status --porcelain":          "",
		wt + "|rev-list --count HEAD --not --remotes=origin": "0\n",
	}}
	items, err := ListGC(g, repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 mrglass worktree (main checkout excluded), got %v", items)
	}
	it := items[0]
	if it.Slug != "api-PROJ-1" || it.Branch != "mrglass/api-PROJ-1" || it.Path != wt || it.Dirty {
		t.Errorf("bad item: %+v", it)
	}
}

func TestListGCMarksDirty(t *testing.T) {
	repo := "/p/api"
	base := DefaultBase(repo)
	wt := filepath.Join(base, "api-2")
	porcelain := "worktree " + wt + "\nHEAD bbb\nbranch refs/heads/mrglass/api-2\n\n"
	g := &gcGit{out: map[string]string{
		repo + "|worktree list --porcelain": porcelain,
		wt + "|status --porcelain":          " M main.go\n",
	}}
	items, err := ListGC(g, repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Dirty {
		t.Errorf("uncommitted changes should mark dirty: %+v", items)
	}
}

func TestListGCMarksUnpushedDirty(t *testing.T) {
	repo := "/p/api"
	base := DefaultBase(repo)
	wt := filepath.Join(base, "api-3")
	porcelain := "worktree " + wt + "\nHEAD ccc\nbranch refs/heads/mrglass/api-3\n\n"
	g := &gcGit{out: map[string]string{
		repo + "|worktree list --porcelain": porcelain,
		wt + "|status --porcelain":          "",
		wt + "|rev-list --count HEAD --not --remotes=origin": "2\n",
	}}
	items, err := ListGC(g, repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Dirty {
		t.Errorf("unpushed commits should mark dirty: %+v", items)
	}
}

func TestRemovable(t *testing.T) {
	items := []GCItem{
		{Slug: "api-OPEN-1"},              // MR still open → keep, not listed
		{Slug: "api-GONE-2"},              // MR gone, clean → remove
		{Slug: "api-GONE-3", Dirty: true}, // MR gone, dirty → skip (reported)
	}
	open := map[string]bool{"api-OPEN-1": true}
	remove, skip := Removable(items, open)
	if len(remove) != 1 || remove[0].Slug != "api-GONE-2" {
		t.Errorf("remove = %+v", remove)
	}
	if len(skip) != 1 || skip[0].Slug != "api-GONE-3" {
		t.Errorf("skip = %+v", skip)
	}
}

func TestRemoveRunsGitCleanup(t *testing.T) {
	g := &gcGit{out: map[string]string{
		"/p/api|worktree remove /p/.mrglass-worktrees/api-2": "",
		"/p/api|branch -D mrglass/api-2":                     "",
		"/p/api|worktree prune":                              "",
	}}
	git := Git{R: g}
	item := GCItem{RepoDir: "/p/api", Path: "/p/.mrglass-worktrees/api-2", Branch: "mrglass/api-2", Slug: "api-2"}
	if err := git.Remove(item); err != nil {
		t.Fatal(err)
	}
	if len(g.calls) != 3 {
		t.Errorf("want 3 git calls, got %v", g.calls)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/worktree/ -run 'GC|Removable|TestRemove' -v`
Expected: FAIL (undefined: GCItem, ListGC, ...).

- [ ] **Step 3: Implement gc.go**

Create `internal/worktree/gc.go`:

```go
package worktree

import (
	"fmt"
	"path/filepath"
	"strings"
)

// GCItem is one mrglass-managed worktree eligible for garbage collection.
type GCItem struct {
	RepoDir string // main clone the worktree belongs to
	Path    string // worktree directory
	Branch  string // "mrglass/<slug>"
	Slug    string
	Dirty   bool // uncommitted changes or commits not on any origin ref
}

// DefaultBase is where `w` creates worktrees when worktree.dir isn't set.
func DefaultBase(repoDir string) string {
	return filepath.Join(filepath.Dir(repoDir), ".mrglass-worktrees")
}

// ListGC finds mrglass-managed worktrees of repoDir: entries of
// `git worktree list --porcelain` on an mrglass/* branch under the worktree
// base. baseDir "" means DefaultBase(repoDir). Each item is checked for
// dirtiness (uncommitted changes, or commits absent from origin) so callers
// can refuse to remove work in progress.
func ListGC(g GitRunner, repoDir, baseDir string) ([]GCItem, error) {
	if baseDir == "" {
		baseDir = DefaultBase(repoDir)
	}
	out, err := g.Run(repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("worktree list: %v", err)
	}
	var items []GCItem
	var path string
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch refs/heads/mrglass/"):
			slug := strings.TrimPrefix(line, "branch refs/heads/mrglass/")
			if !strings.HasPrefix(path, baseDir+string(filepath.Separator)) {
				continue // an mrglass/* branch checked out elsewhere: not ours to GC
			}
			items = append(items, GCItem{
				RepoDir: repoDir,
				Path:    path,
				Branch:  "mrglass/" + slug,
				Slug:    slug,
				Dirty:   isDirty(g, path),
			})
		}
	}
	return items, nil
}

// isDirty reports uncommitted changes or commits not present on any origin
// ref. Errors count as dirty — when in doubt, don't delete.
func isDirty(g GitRunner, dir string) bool {
	out, err := g.Run(dir, "status", "--porcelain")
	if err != nil || strings.TrimSpace(string(out)) != "" {
		return true
	}
	out, err = g.Run(dir, "rev-list", "--count", "HEAD", "--not", "--remotes=origin")
	if err != nil || strings.TrimSpace(string(out)) != "0" {
		return true
	}
	return false
}

// Removable splits items into removable (MR no longer open, worktree clean)
// and skipped-dirty (MR no longer open but has local work). Items whose MR is
// still open are excluded from both.
func Removable(items []GCItem, openSlugs map[string]bool) (remove, skipDirty []GCItem) {
	for _, it := range items {
		if openSlugs[it.Slug] {
			continue
		}
		if it.Dirty {
			skipDirty = append(skipDirty, it)
			continue
		}
		remove = append(remove, it)
	}
	return remove, skipDirty
}

// Remove deletes a GC'd worktree: the worktree itself, its mrglass/* branch,
// then a prune. Not forced — a clean check happened in Removable; if git still
// refuses, surface that instead of deleting work.
func (g Git) Remove(item GCItem) error {
	if out, err := g.R.Run(item.RepoDir, "worktree", "remove", item.Path); err != nil {
		return fmt.Errorf("worktree remove %s: %v: %s", item.Path, err, strings.TrimSpace(string(out)))
	}
	if out, err := g.R.Run(item.RepoDir, "branch", "-D", item.Branch); err != nil {
		return fmt.Errorf("branch -D %s: %v: %s", item.Branch, err, strings.TrimSpace(string(out)))
	}
	if _, err := g.R.Run(item.RepoDir, "worktree", "prune"); err != nil {
		return fmt.Errorf("worktree prune: %v", err)
	}
	return nil
}
```

Note the test fake errors on unexpected calls — `TestListGCMarksDirty` relies on `isDirty` short-circuiting after a non-empty `status --porcelain` (no rev-list call), and the "errors count as dirty" rule.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/worktree/ -v`
Expected: PASS (including pre-existing Prepare/launch tests).

- [ ] **Step 5: Commit**

```bash
git add internal/worktree/gc.go internal/worktree/gc_test.go
git commit -m "feat(worktree): GC discovery, dirty-guarded classification, removal"
```

---

### Task 13: `mrglass --gc` CLI wiring

**Files:**
- Create: `cmd/mrglass/gc.go`
- Create: `cmd/mrglass/gc_test.go`
- Modify: `cmd/mrglass/main.go` (flags + dispatch)

**Interfaces:**
- Consumes: Task 12's `worktree.ListGC`, `worktree.Removable`, `worktree.Git.Remove`, `worktree.DefaultBase`; `provider.Provider.List`; `worktree.Slug`.
- Produces: `runGC(cfg config.Config, p provider.Provider, me string, dryRun bool, in io.Reader, out io.Writer) error`, and `--gc` / `--dry-run` flags in main.

- [ ] **Step 1: Write failing test for runGC's plan/confirm flow**

Create `cmd/mrglass/gc_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whitel1ght/mrglass/internal/config"
	"github.com/whitel1ght/mrglass/internal/core"
)

type fakeProvider struct{ mrs []core.MR }

func (f fakeProvider) Whoami() (string, error) { return "me", nil }
func (f fakeProvider) List(me string, days int, ticketPattern string) ([]core.MR, error) {
	return f.mrs, nil
}

func TestRunGCDryRunListsWithoutPrompting(t *testing.T) {
	// A projectsDir with one "repo" (a dir containing .git).
	projects := t.TempDir()
	repo := filepath.Join(projects, "api")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.ProjectsDir = projects

	var out bytes.Buffer
	// No git worktrees exist → plan is empty; dry-run must not read stdin.
	err := runGC(cfg, fakeProvider{}, "me", true, strings.NewReader(""), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing to remove") {
		t.Errorf("empty plan should say so, got %q", out.String())
	}
}

func TestRunGCNoAnswerRemovesNothing(t *testing.T) {
	projects := t.TempDir()
	cfg := config.Default()
	cfg.ProjectsDir = projects
	var out bytes.Buffer
	if err := runGC(cfg, fakeProvider{}, "me", false, strings.NewReader("n\n"), &out); err != nil {
		t.Fatal(err)
	}
}
```

(Deeper flows — real worktrees, confirmation "y" — are covered by `internal/worktree` unit tests; runGC's own logic is discovery + printing + prompt, tested at that level. Keep gc.go thin enough that this is honest.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/mrglass/ -v`
Expected: FAIL (undefined: runGC).

- [ ] **Step 3: Implement cmd/mrglass/gc.go**

```go
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/whitel1ght/mrglass/internal/config"
	"github.com/whitel1ght/mrglass/internal/provider"
	"github.com/whitel1ght/mrglass/internal/worktree"
)

// gcListDays effectively disables List's updated-after window: GC must see ALL
// the user's open MRs, or a stale-but-open MR's worktree would look removable.
const gcListDays = 3650

// runGC finds mrglass-managed worktrees whose MR is no longer open, prints the
// plan, and (unless dryRun) removes them after one y/N confirmation. Dirty
// worktrees (uncommitted or unpushed work) are reported and never removed.
func runGC(cfg config.Config, p provider.Provider, me string, dryRun bool, in io.Reader, out io.Writer) error {
	mrs, err := p.List(me, gcListDays, cfg.TicketRegex)
	if err != nil {
		return fmt.Errorf("listing open MRs: %w", err)
	}
	openSlugs := map[string]bool{}
	for _, mr := range mrs {
		openSlugs[worktree.Slug(mr)] = true
	}

	git := worktree.New()
	var remove, skip []worktree.GCItem
	for _, repo := range gcRepos(cfg) {
		items, err := worktree.ListGC(git.R, repo, expandHome(cfg.Worktree.Dir))
		if err != nil {
			fmt.Fprintf(out, "⚠ %s: %v\n", repo, err)
			continue
		}
		r, s := worktree.Removable(items, openSlugs)
		remove, skip = append(remove, r...), append(skip, s...)
	}

	for _, it := range skip {
		fmt.Fprintf(out, "skip  %s  [dirty — has local work]\n", it.Path)
	}
	if len(remove) == 0 {
		fmt.Fprintln(out, "nothing to remove")
		return nil
	}
	for _, it := range remove {
		fmt.Fprintf(out, "remove %s  (branch %s; MR no longer open)\n", it.Path, it.Branch)
	}
	if dryRun {
		return nil
	}

	fmt.Fprintf(out, "Remove %d worktree(s)? [y/N] ", len(remove))
	sc := bufio.NewScanner(in)
	if !sc.Scan() || strings.ToLower(strings.TrimSpace(sc.Text())) != "y" {
		fmt.Fprintln(out, "aborted")
		return nil
	}
	for _, it := range remove {
		if err := git.Remove(it); err != nil {
			fmt.Fprintf(out, "⚠ %v\n", err)
			continue
		}
		fmt.Fprintf(out, "✓ removed %s\n", it.Path)
	}
	return nil
}

// gcRepos lists git repos one level under projectsDir — the clones whose
// sibling .mrglass-worktrees (or worktree.dir) may hold `w` worktrees.
func gcRepos(cfg config.Config) []string {
	root := expandHome(cfg.ProjectsDir)
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var repos []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			repos = append(repos, dir)
		}
	}
	return repos
}

// expandHome expands a leading ~/ to the user's home directory.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
```

Note the import block must NOT include `core` (nothing in this file names a core type directly — `worktree.Slug(mr)` takes the `core.MR` values returned by `p.List`, whose type is inferred). Verify with `go build ./cmd/mrglass/`.

- [ ] **Step 4: Wire the flags in main.go**

In `cmd/mrglass/main.go`, add to the flag block:

```go
		gcFlag = flag.Bool("gc", false, "remove worktrees of merged/closed MRs (with confirmation) and exit")
		dryRun = flag.Bool("dry-run", false, "with --gc: print what would be removed, remove nothing")
```

and after the `me, err := p.Whoami()` auth gate (so GC gets an authenticated provider), before the analyzer wiring:

```go
	if *gcFlag {
		if err := runGC(cfg, p, me, *dryRun, os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "gc:", err)
			os.Exit(1)
		}
		return
	}
```

- [ ] **Step 5: Run tests + build**

Run: `go test ./cmd/mrglass/ -v && go build ./... && go vet ./...`
Expected: PASS / clean.

- [ ] **Step 6: Smoke-test dry-run against the real setup**

Run: `go run ./cmd/mrglass --gc --dry-run`
Expected: either a plan/"nothing to remove" listing, or the auth-gate error if `glab` isn't authenticated in this environment. Confirm it does NOT prompt and does NOT remove anything. Report the actual output.

- [ ] **Step 7: Commit**

```bash
git add cmd/mrglass/
git commit -m "feat(cli): mrglass --gc removes worktrees of closed MRs (confirmed, dirty-safe)"
```

---

### Task 14: Docs + final verification

**Files:**
- Modify: `README.md` (mention `--gc`, statusline style/states/grow now honored)
- Modify: `config/config.example.yaml` (already touched in Tasks 6/8; verify consistency)

**Interfaces:**
- Consumes: everything above.
- Produces: docs matching behavior; a green, formatted tree.

- [ ] **Step 1: Update README**

In `README.md` "Quick start" section, after `mrglass --config ...` add:

```markdown
mrglass --gc         # clean up worktrees of merged/closed MRs (add --dry-run to preview)
```

In the "Work on it (`w`)" section, append:

```markdown
Worktrees persist so you can return to them; when their MRs are merged or
closed, reclaim them with `mrglass --gc` (dirty worktrees — uncommitted or
unpushed work — are never removed).
```

- [ ] **Step 2: Cross-check config.example.yaml against the code**

Read `config/config.example.yaml` end-to-end and confirm: no `align:` anywhere; the `states:` comment block from Task 8 present; `style: faint` on age still there (now honored). Fix any drift.

- [ ] **Step 3: Full verification**

Run:
```bash
gofmt -l . && go vet ./... && go test -race ./... && go build ./...
```
Expected: `gofmt -l` prints nothing; vet clean; all tests pass with race detector; build succeeds.

Run: `go test -cover ./... | grep -E 'gitlab|coverage'` — confirm gitlab ≥ 75%.

- [ ] **Step 4: Commit**

```bash
git add README.md config/config.example.yaml
git commit -m "docs: --gc, statusline style/states/grow"
```

- [ ] **Step 5: Verify the whole branch**

Run: `git log --oneline main..HEAD`
Expected: ~11 commits matching Tasks 1-14. The branch is ready for the finishing-a-development-branch flow (merge/PR decision belongs to the user).
