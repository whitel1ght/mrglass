# mrglass — Copy Review to Clipboard (Design)

_Date: 2026-07-10 · Status: approved_

In the review confirmation view, `c` copies the generated review markdown to
the system clipboard and stays in the confirm view (status `✓ review
copied`), so the user can copy-and-post or copy-and-discard. Prompt line
gains `/ [c]opy`.

Clipboard: shell-out matching the openURL pattern — `pbcopy` (macOS),
`clip` (Windows), first of `wl-copy`/`xclip`/`xsel` (Linux); review text
piped via stdin. Missing tool / failure → `⚠ copy failed: …` in the status.
`copyCmd(text) tea.Cmd` → `copyResultMsg{err}`; the exec seam is a
package-level `clipboardRun` var, overridable in tests.

Tests: `c` in confirm mode returns a copy command and keeps pendingReview;
the exact review text reaches the clipboard runner; success and failure both
surface in the status; `c` in list mode still starts a review.
