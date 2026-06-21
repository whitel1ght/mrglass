# mrglass

An interactive terminal dashboard (htop/mole-style) for your active GitLab merge
requests — vim-style navigation, a configurable per-MR statusline, themes, background
auto-refresh, desktop notifications, and **Claude triage** that explains meaningful MR
changes and what to do about them, spending tokens only when there's something worth
reasoning about.

Successor to the [`gitlab-mr-status`](../gitlab-mr-status) prototype, reimplemented in Go.

> **Status: design phase.** No code yet. See the design spec:
> [`docs/superpowers/specs/2026-06-21-mrglass-design.md`](docs/superpowers/specs/2026-06-21-mrglass-design.md).

## At a glance (planned v1)

- Single Go binary (Bubble Tea), architected after `gh-dash`.
- GitLab provider behind a clean interface (`glab` under the hood); GitHub later.
- Configurable **sections** (saved `expr` filters), **statusline** (declarative segment
  list), and **themes** — runs with zero config.
- **Token-tiered Claude**: free deterministic diff (Tier 0) → cheap Claude triage of
  meaningful changes (Tier 1), gated by a pure-Go pre-filter. Agentic propose-and-confirm
  fix (Tier 2) is a later spec.

## Prerequisites (planned)

- [`glab`](https://gitlab.com/gitlab-org/cli), authenticated — required.
- `claude` (Claude Code CLI) — optional, enables triage.
- A desktop notifier (`osascript` on macOS / `notify-send` on Linux) — optional.
