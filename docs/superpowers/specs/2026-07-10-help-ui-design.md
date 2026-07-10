# mrglass — Nicer Hotkey Help (Design)

_Date: 2026-07-10 · Status: approved_

The always-on short-help bar renders at ~210 cols (verbose descriptions) and
crops on any real terminal. Two changes:

## A — always-fit bottom bar

`KeyMap.ShortHelp()` returns a compact 6-binding set with terse labels
(distinct from the descriptive FullHelp labels): j/k move · enter open ·
c review · ⌫ hide · r refresh · ? help. ~45 cols, fits any terminal. Add a
short label to each via a helper so FullHelp keeps its descriptive text.

## C — bordered, grouped help overlay ('?')

Replace the bare FullHelpView with a centered panel (theme-accent rounded
border, padded): title "mrglass — keys", then 3 groups (Navigation /
Actions / App) each with a subtle header and aligned two-column rows
(key in accent, right-padded to the group's widest key; description in
subtle), blank line between groups, footer "? / esc to close". Centered via
lipgloss.Place over the full terminal. '?', esc, and q close it.

## Tests

- ShortHelp bar width <= 60 and includes the core keys.
- Help overlay contains every binding key, the three group headers, and a
  border character.
- esc closes the overlay (showHelp back to false).
