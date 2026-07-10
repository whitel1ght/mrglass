# mrglass — Hide MRs (backspace) + Hidden Tab (Design)

_Date: 2026-07-10 · Status: approved_

Let the user hide noisy MRs from the dashboard with `backspace`. Hidden MRs
collect in a synthetic **Hidden (n)** tab (visible only when non-empty),
where `backspace` restores them. Hidden = fully muted.

## Decisions

- **Persist** across restarts: hidden refs stored as sorted JSON array in a
  sibling of the state file (`core.HiddenPath(statePath)` →
  `.mrglass-state-hidden.json`). Missing file → empty set; corrupt → empty +
  warning. Saved on every toggle. **No auto-pruning** — pruning could
  un-hide an MR that merely fell out of the `days` fetch window.
- **Mute entirely**: `watch.Deps` gains the hidden set; `Fetch` drops
  changes for hidden refs after diffing, so they produce no desktop
  notifications and no triage. Snapshots still track every MR, so unhiding
  never replays stale notifications.

## TUI

- `keys.Hide` = backspace, in the help bar.
- Normal tab + backspace → hide selected (status names the Hidden tab);
  Hidden tab + backspace → restore.
- The Hidden tab renders after the configured sections, with a count badge,
  cycling normally via tab/h/l; it disappears when empty (section index
  clamps). Normal sections and their count badges exclude hidden MRs.
- Save errors surface in the status footer.

## Testing

- core: Load/SaveHidden round-trip; missing file → empty.
- watch: a triage-worthy change on a hidden ref is absent from
  `FetchResult.Changes` (this is what mutes both notify and triage).
- tui: hide removes the MR from its section and shows "Hidden (1)"; the
  hidden tab lists it; unhide restores and the tab disappears; the hidden
  file is written (temp paths in tests).
