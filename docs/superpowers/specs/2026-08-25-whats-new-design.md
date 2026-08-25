# mrglass — "What's New" (highlight MRs changed since last look) Design

_Date: 2026-08-25 · Status: approved_

When the user returns to mrglass (tmux tab switch, back from browser) or hits
`r`, they should immediately see which MRs changed since they last looked.
The diff data already exists (`res.Changes`); today it's used for
notify/triage then dropped. This feature retains it, highlights changed rows,
and clears the highlight once the user views the MR.

## Trigger

Add `tea.WithReportFocus()` to `tea.NewProgram` (cmd/mrglass/main.go). Handle
`tea.FocusMsg` in Update by issuing the same refresh as `r`
(startBusy("fetch") + fetchCmd). Focus events silently no-op on Terminal.app
and tmux without `set -g focus-events on`, so the existing `r` key and
auto-refresh tick remain the reliable baseline. `tea.BlurMsg` is ignored.

## Change tracking (session-only, in-memory)

Model gains `changed map[string]core.ChangeKind` (mirrors `expanded`; NOT
persisted). In the `fetchResultMsg` handler, union `res.Changes` into it,
keyed by Ref, storing the Kind (KindNew vs KindChanged). The first fetch
yields no changes (Diff needs a prior snapshot), so no startup highlight
storm. On each apply, drop entries whose ref is no longer in the fetched
list (gone MRs).

## "Seen" reset (airtight/idempotent)

Delete a ref from `changed` when the user views it:
- Expand it (`enter` / keys.Expand) — on OPEN (not collapse).
- Open in browser (`o` / keys.Open).
New key `keys.SeenAll` = `S` clears the entire `changed` map ("mark all seen").

## Visual (gutter marker + bold title)

Thread `Changed bool` and `New bool` into `statusline.RowView` (mirror
`HasAdvice`). In the app row loop (marker computation), for a ref in
`changed`:
- KindNew: marker `●` in Accent, title bold+Accent.
- KindChanged: marker `●` in Warn, title bold.
Unchanged rows keep the `▸`/`▾` disclosure marker. Marker stays 2 cols
(no layout shift). The title-bold is applied in the statusline `text`
segment when RowView.Changed.

## Non-goals

No persistence across restarts; no jump-to-next-changed; no Changed filter
view. Footer count optional, deferred.

## Tests

- watch/diff already tested; here at TUI level:
- fetchResultMsg unions changes into m.changed; first fetch (no prior) adds
  nothing.
- a changed ref shows the ● marker + bold title; unchanged shows ▸.
- KindNew vs KindChanged select accent vs warn marker.
- expand-open clears that ref; expand-collapse does not re-add.
- open (`o`) clears that ref.
- `S` clears all.
- a ref absent from a later fetch is dropped from m.changed.
- tea.FocusMsg returns a fetch command.

## Addendum (2026-08-25): per-tab "has new" indicator

Both tab rows mark tabs that contain changed MRs, so the user can spot which
tab/project has new activity without visiting each:
- Status tabs: a `●` (Warn/amber) prefix when any MR in `m.changed` matches
  that section's filter (within the active project scope).
- Project tabs: a `●` prefix on an INACTIVE project tab when any changed MR
  belongs to that project (`All` = any). The active project tab omits the dot
  (already amber; you're viewing it).

Computed at render time from the existing `changed` set — no new state. Cleared
by the same view/seen paths. Tests: status tab dots for an off-screen section
change; project tab dots for an off-screen project change; dot gone after `S`.
