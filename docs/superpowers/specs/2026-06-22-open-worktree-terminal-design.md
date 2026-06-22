# Open MR branch in a terminal (worktree + configurable launcher)

_Date: 2026-06-22 · Branch: feat/open-worktree-terminal · Status: approved_

Press `w` on an MR to open its project — checked out on the MR branch in a
dedicated git worktree — in a new tmux window / terminal tab / terminal window
(configurable), running Claude (configurable). Jump from reviewing an MR to
working on it in one keypress.

## Decisions

- **Branch handling: dedicated PERSISTENT worktree per MR.** Unlike the review
  feature's throwaway *detached* worktree, this creates a named worktree on a
  local branch tracking the MR head, so the user can edit/commit/push and the
  work survives. The main clone is never touched; multiple MRs open in parallel.
- **Open mechanism: configurable command template** (`openCommand`) with
  placeholders mrglass fills — works for tmux / iTerm / Terminal / kitty /
  wezterm with zero per-tool code.
- **Command to run: configurable `workCmd`** (default `claude`), substituted as
  `{cmd}`.
- **Hotkey `w`** ("work on it").
- **Graceful**: missing `openCommand` → "set worktree.openCommand in config";
  no local clone → "no local clone of <repo>"; worktree prep fails → show the
  error. Never blocks the UI.

## Config
```yaml
worktree:
  # Command run (detached) to open the MR worktree. Placeholders mrglass fills:
  #   {dir}    absolute worktree path (cd into this)
  #   {cmd}    the workCmd below
  #   {branch} the MR source branch
  #   {key}    the ticket key (or the MR ref if none)
  # Examples:
  #   tmux new window:   tmux new-window -c {dir} {cmd}
  #   tmux new session:  tmux new-session -d -s {key} -c {dir} {cmd}
  #   iTerm new tab / Terminal new window: via osascript (see example config)
  openCommand: ""        # empty disables `w`
  workCmd: "claude"      # what to run in the worktree ({cmd})
  # Where worktrees are created. Default: <clone>/../.mrglass-worktrees/
  dir: ""                # optional override (absolute)
```

## Design

### Reuse
- `review.ResolveDir(mr, projectsDir, projectPaths)` already finds the local
  clone — reuse it (move/share, don't duplicate). The MR-head fetch ref is
  `merge-requests/<iid>/head` (GitLab) — note GitHub uses `pull/<n>/head`; v1
  targets the same forge-aware fetch (see Forge note).

### New package `internal/worktree`
Separate from the review worktree (different lifecycle: persistent + named +
on-branch, vs throwaway + detached).
```go
// Prepare ensures a persistent worktree for the MR branch exists and returns its
// path. Idempotent: if it already exists for this MR, returns it as-is.
//   repoDir   the local clone
//   branch    the MR source branch name
//   fetchRef  forge fetch ref for the MR head (e.g. "merge-requests/12/head")
//   baseDir   where to put worktrees ("" → <repoDir>/../.mrglass-worktrees)
//   slug      stable per-MR name (e.g. "ecfx-k8s-ECFX-1234")
// Returns (dir, error).
func Prepare(repoDir, branch, fetchRef, baseDir, slug string) (string, error)
```
Mechanics (via git CLI, each step's error surfaced):
1. `git -C repoDir fetch origin <fetchRef>:refs/mrglass/<slug>` (or fetch the
   branch). Create/update a local branch for the MR.
2. worktree dir = `baseDir/<slug>`. If it already exists (git worktree list),
   reuse. Else `git -C repoDir worktree add <dir> <localBranch>`.
3. Return dir. (No auto-remove — persistent. A future `W`/cleanup can prune.)

Behind a `Worktreer` interface for testing with a fake git runner.

### Launcher (`internal/worktree/launch.go` or in tui)
```go
// Launch runs the openCommand template with placeholders filled, detached.
func Launch(openCommand, dir, workCmd, branch, key string) error
```
- Substitute {dir}/{cmd}/{branch}/{key} in the template.
- Split into argv (shell-style) and run **detached** (Start + background Wait),
  like openURL — no terminal flash, no blocking.
- For tmux templates this just runs `tmux new-window …` against the running
  server; for AppleScript it runs `osascript -e …`.

### Config (`internal/config/config.go`)
Add `Worktree WorktreeConfig { OpenCommand, WorkCmd, Dir string }`. Default
WorkCmd "claude" in Default().

### TUI (`internal/tui/app.go`)
- Model gains the resolved deps (reuses cfg.ProjectsDir/ProjectPaths for clone
  resolution; worktree config from cfg.Worktree).
- Key `w` (`keys.OpenWork`): on the selected MR:
  - openCommand empty → status "⚠ set worktree.openCommand in config".
  - resolve clone; not found → "no local clone of <repo>".
  - else fire an async `worktreeCmd(mr)`: Prepare the worktree, then Launch.
    Report success ("opened <slug> on <branch>") or the error via a
    `worktreeMsg{slug, err}`. (Async because git fetch/worktree-add can take a
    second; don't block the UI.)
- `slug`: `<repo-name>-<ticketKey-or-iid>`, filesystem-safe.

### Forge note (GitLab/GitHub)
The fetch ref differs by forge: GitLab `merge-requests/<iid>/head`, GitHub
`pull/<number>/head`. v1: derive it from the configured forge (cfg.Forge) +
the MR (IID for gitlab; number-from-ref for github). Keep this in one helper so
adding forges is a switch, not scattered logic.

## Testing
- config: worktree fields parse; WorkCmd default.
- worktree.Prepare: fake git runner — fetch + worktree-add args; idempotent
  reuse when the dir already exists; error surfaced on git failure.
- Launch: template substitution ({dir}/{cmd}/{branch}/{key}) and argv split; a
  fake runner captures the command. tmux + osascript template shapes.
- tui: `w` with no openCommand → config-prompt status, no command; with config +
  resolvable clone → fires worktreeCmd; no clone → "no local clone" status.
- forge fetch-ref helper: gitlab vs github ref shape.

## Out of scope
- Worktree cleanup/listing UI (a later `W` or command). Worktrees persist.
- Auto-detecting the terminal; the template is explicit by design.
