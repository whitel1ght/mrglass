# mrglass — Project Filter (second tab row) Design

_Date: 2026-07-10 · Status: approved_

A second, orthogonal tab row filters MRs by project, composing with the
existing status sections. Two independent axes: status (tab/h/l) × project
([ / ]).

## Core

- `core.ProjectOf(ref) string` — the ref prefix before `!` or `#`
  (`group/project!177` → `group/project`; `owner/repo#42` → `owner/repo`;
  a ref with neither returns the whole string).
- `MR.Project()` convenience wrapping ProjectOf(m.Ref).

## TUI — project tab row

- Rendered below the status tabs (same header block, joined with "\n" into
  tabBar so chrome height stays correct). Appears only when ≥2 distinct
  projects are present among the visible (non-hidden) MRs.
- Tabs: `All` first (no filter), then distinct project paths alphabetical.
  Label = last path segment for compactness; if two selected projects share
  the same last segment, disambiguate with the full path.
- Selection stored by project PATH (`projectFilter string`, "" = All), so it
  survives refreshes and persists across status-tab switches. If the selected
  project disappears from the list, fall back to All.
- `]` next project, `[` prev, wrapping over [All, …projects].

## Filtering

`refilter` applies the project predicate BEFORE the section filter and before
the Hidden tab logic's base set: base = visibleMRs (non-hidden), then keep
`ProjectOf(ref) == projectFilter` unless projectFilter is "". Status count
badges count within the selected project. Hidden tab is unaffected by the
project filter (hidden is its own axis) — simplest and least surprising.

## Help

`[` / `]` added to the FullHelp Navigation group.

## Tests

- ProjectOf: gitlab ref, github ref, no-separator ref.
- project row: distinct + sorted + All-first; hidden when <2 projects;
  excludes hidden MRs from the project set.
- `]`/`[` cycle and wrap; projectFilter persists across a status tab switch.
- filtering composes with a status section (only that project's MRs shown)
  and status badges reflect the selected project.
- selected project vanishing on refresh falls back to All.
