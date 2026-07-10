# mrglass

An interactive terminal dashboard for your open **GitLab / GitHub** merge/pull
requests — vim-style navigation, a configurable per-MR statusline, themes,
background auto-refresh, and deep **Claude** integration:

- **Triage** — Claude explains meaningful MR changes and what to do, spending
  tokens only when there's something worth reasoning about.
- **Review** (`c`) — generate a project-aware Claude review (using your local
  clone for full context, optionally driven by a review *skill*), shown for your
  confirmation before it's posted as a comment.
- **Work on it** (`w`) — check the MR branch out in a dedicated git worktree and
  open it in a terminal (tmux / iTerm / Terminal / kitty / wezterm) running
  Claude.
- **Tickets** — open the linked issue in your browser (`J`, any tracker via a URL
  template) and see inline Jira status when you expand an MR.
- **Hide noise** — `backspace` hides an MR into a **Hidden** tab (persisted,
  fully muted: no notifications, no triage); `backspace` there restores it.

## Install

### Homebrew (macOS / Linux)

```bash
brew tap whitel1ght/tap
brew install mrglass
```

### go install

```bash
go install github.com/whitel1ght/mrglass/cmd/mrglass@latest
```

### From source

```bash
git clone https://github.com/whitel1ght/mrglass
cd mrglass
go build -o mrglass ./cmd/mrglass
```

## Prerequisites

- **A forge CLI, authenticated** (required):
  - GitLab → [`glab`](https://gitlab.com/gitlab-org/cli) (`glab auth login`)
  - GitHub → [`gh`](https://cli.github.com) (`gh auth login`)
- `claude` (Claude Code CLI) on `PATH` — optional; enables triage, review, and `w`.
- A terminal multiplexer / emulator for `w` (e.g. `tmux`) — optional.

## Quick start

```bash
mrglass              # opens the dashboard
mrglass --version
mrglass --config /path/to/config.yaml
mrglass --gc         # clean up worktrees of merged/closed MRs (add --dry-run to preview)
```

It runs with **no config file** (sensible defaults, GitLab). To customize, create
a config file — see below.

## Configuration

mrglass reads `~/.config/mrglass/config.yaml` (or `$XDG_CONFIG_HOME/mrglass/`).
**Everything is optional** — defaults are shown in
[`config/config.example.yaml`](config/config.example.yaml). Copy it to start:

```bash
mkdir -p ~/.config/mrglass
curl -fsSL https://raw.githubusercontent.com/whitel1ght/mrglass/main/config/config.example.yaml \
  -o ~/.config/mrglass/config.yaml
```

### Pick your forge

```yaml
forge: gitlab        # gitlab (via glab) | github (via gh)
```

### Tickets (any tracker)

```yaml
tickets:
  # Press J to open the linked ticket. {key} is replaced with the ticket key.
  #   Jira:   https://acme.atlassian.net/browse/{key}
  #   Linear: https://linear.app/acme/issue/{key}
  #   GitHub: https://github.com/acme/repo/issues/{key}
  urlTemplate: "https://acme.atlassian.net/browse/{key}"

  # Inline ticket status when you expand an MR (Jira only for now).
  status: jira                                  # none | jira
  jiraBaseURL: "https://acme.atlassian.net"
```

Inline Jira status needs API credentials in the **environment** (never in the
config file):

```bash
export JIRA_EMAIL="you@company.com"
export JIRA_API_TOKEN="…"   # id.atlassian.com/manage-profile/security/api-tokens
```

### Claude review (`c`)

```yaml
projectsDir: ~/projects          # where your local clones live (for full context)
reviewPrompt: >                  # or drive it with a skill:
  ...
# reviewSkill: superpowers:requesting-code-review
```

The review checks the MR branch out in a throwaway worktree under your local
clone, runs Claude there (so it sees the repo's `CLAUDE.md`, skills, and files),
and shows the result for confirmation before posting. Falls back to diff-only if
no clone is found.

### Work on it (`w`)

```yaml
worktree:
  # {dir}=worktree path, {cmd}=workCmd, {branch}, {key}, {session}=current tmux session
  openCommand: "tmux new-window -t {session}: -c {dir} {cmd}"
  workCmd: "claude"
```

Presets for iTerm, Terminal, kitty, and wezterm are in the example config.

Worktrees persist so you can return to them; when their MRs are merged or
closed, reclaim them with `mrglass --gc` (dirty worktrees — uncommitted or
unpushed work — are never removed).

### Sections, statusline, theme

Configurable saved-filter sections, a declarative per-row statusline, and themes
(`tokyonight` | `default` | `dracula`) — all documented in the example config.

## Keys

| Key | Action |
|-----|--------|
| `j`/`k`, `g`/`G` | move / top / bottom |
| `tab` | switch section |
| `enter` | expand / collapse an MR |
| `o` | open the MR in the browser |
| `J` | open the linked ticket |
| `c` | Claude review (propose → confirm → post) |
| `w` | open the MR branch in a terminal worktree |
| `t` / `a` | triage this MR / toggle auto-triage |
| `r` | refresh |
| `?` | help |
| `q` | quit |

## License

MIT — see [LICENSE](LICENSE).
