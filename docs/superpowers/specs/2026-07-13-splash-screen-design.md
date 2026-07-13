# mrglass — Startup Splash Screen (Design)

_Date: 2026-07-13 · Status: approved_

Before the first fetch returns, the screen is mostly empty with a small
loader. Replace it with a centered logo (magnifying glass + block "mrglass"
wordmark) that vanishes the instant MRs load.

## When

In View(), after the width==0 / help / pendingReview guards, if NOT m.loaded
render the splash instead of the empty dashboard body. Flips off the moment
fetchResultMsg sets m.loaded = true.

## What

internal/tui/splash.go: a `logo` const (round magnifying glass beside a
shadow-font "mrglass", handle trailing down-right, tagline below) and
`(m Model) splashView() string`. The whole logo is centered as ONE block via
lipgloss.Place(width, height, Center, Center, ...). Glass+wordmark in Accent,
tagline in Subtle. A spinner + "loading your merge requests…" line under the
tagline shows it's working.

## Degradation

If the terminal is narrower or shorter than the logo, fall back to a compact
"mrglass · loading…" centered line rather than a wrapped/broken logo.

## Tests

- splashView is shown before load (contains a wordmark marker + loading line)
  and absent after fetchResultMsg (loaded) — normal dashboard renders.
- narrow terminal → compact fallback, no line exceeds width.
- error/help/pendingReview states still bypass the splash.
