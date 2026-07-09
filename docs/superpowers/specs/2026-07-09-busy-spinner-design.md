# mrglass — Footer Activity Spinner (Design)

_Date: 2026-07-09 · Status: approved_

Async operations (review, refresh, post, triage, worktree open, Jira fetch)
currently show only a static gray status string — it is not obvious anything
is running. Add an animated spinner + accent-colored label to the status
footer while any operation is in flight.

## Design

- `Model` gains `spinner.Model` (bubbles/spinner `MiniDot`, already a
  dependency) and `busy map[string]string` (operation key → display label).
- Every async command start registers a label and returns `spinner.Tick`
  batched with the work command. Keys: `fetch`, `review`, `post`,
  `triage:<ref>`, `worktree`, `jira:<key>`.
- Every result message (`fetchResultMsg`, `reviewMsg`, `postResultMsg`,
  `adviceMsg`, `worktreeMsg`, `jiraMsg`) deletes its entry.
- `spinner.TickMsg`: ignored when `busy` is empty (animation stops, zero idle
  cost); otherwise updates the spinner and re-ticks.
- Footer: while busy, `⠼ label1 · label2` rendered in the theme **Accent**
  style (labels sorted by key for stable order), followed by the usual
  `· auto-triage ON/OFF` suffix in gray. When idle, the footer is unchanged
  (last status text). Footer stays one line; chrome math unaffected.

## Testing

- Pressing `c` (review wired): `View()` shows accent "reviewing <ref>" label;
  delivering `reviewMsg` removes it.
- Refresh (`r`) and post (`y`) labels appear/disappear the same way.
- `spinner.TickMsg` while idle returns no follow-up command.
