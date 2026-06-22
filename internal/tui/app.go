package tui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dmitry/mrglass/internal/analyze"
	"github.com/dmitry/mrglass/internal/config"
	"github.com/dmitry/mrglass/internal/core"
	"github.com/dmitry/mrglass/internal/provider"
	"github.com/dmitry/mrglass/internal/review"
	"github.com/dmitry/mrglass/internal/tui/detailpane"
	"github.com/dmitry/mrglass/internal/tui/keys"
	"github.com/dmitry/mrglass/internal/tui/section"
	"github.com/dmitry/mrglass/internal/tui/statusline"
	"github.com/dmitry/mrglass/internal/tui/theme"
	"github.com/dmitry/mrglass/internal/watch"
)

type fetchResultMsg watch.FetchResult
type fetchErrMsg struct{ err error }
type adviceMsg analyze.Advice
type tickMsg time.Time
type openErrMsg struct{ err error }
type reviewMsg review.Result
type postResultMsg struct {
	ref string
	err error
}

// pending holds a generated review awaiting the user's post/discard decision.
type pending struct {
	ref  string
	mr   core.MR
	text string
}

type Model struct {
	cfg       config.Config
	provider  provider.Provider
	me        string
	analyzer  analyze.Analyzer // may be nil
	reviewer  review.Reviewer  // may be nil (no claude)
	reviewGL  review.GitLab    // forge diff/post capability; may be nil
	statePath string

	keys   keys.KeyMap
	help   help.Model
	styles theme.Styles

	allMRs     []core.MR // full fetched list (provider-agnostic, unfiltered)
	mrs        []core.MR // current active-section rows (flattened, grouped order)
	cursor     int
	sectionIdx int
	loaded     bool            // true once the first fetch has returned
	expanded   map[string]bool // MR ref -> inline detail shown
	advice     map[string]string

	pendingReview *pending      // non-nil while awaiting post/discard confirmation
	reviewVP      viewport.Model // scrollable view of the pending review
	reviewing     bool          // a review is in flight

	autoTriage bool
	status     string
	showHelp   bool
	width      int
	height     int
}

// WithReview wires the on-demand Claude review feature. Both must be non-nil for
// the 'c' hotkey to do anything; main passes nil for either when unavailable.
func (m Model) WithReview(rv review.Reviewer, gl review.GitLab) Model {
	m.reviewer = rv
	m.reviewGL = gl
	return m
}

func New(cfg config.Config, p provider.Provider, me string, az analyze.Analyzer, statePath string) Model {
	return Model{
		cfg: cfg, provider: p, me: me, analyzer: az, statePath: statePath,
		keys: keys.Default(), help: help.New(),
		styles:     theme.BuildStyles(theme.Get(cfg.Theme)),
		advice:     map[string]string{},
		expanded:   map[string]bool{},
		autoTriage: cfg.AutoTriage && az != nil,
		status:     "loading…",
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.fetchCmd()}
	if m.cfg.RefreshMinutes > 0 {
		cmds = append(cmds, m.tickCmd())
	}
	return tea.Batch(cmds...)
}

func (m Model) fetchCmd() tea.Cmd {
	d := watch.Deps{Provider: m.provider, Me: m.me, StatePath: m.statePath, Cfg: m.cfg}
	return func() tea.Msg {
		if m.provider == nil {
			return fetchResultMsg(watch.FetchResult{})
		}
		return fetchResultMsg(watch.Fetch(d))
	}
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(time.Duration(m.cfg.RefreshMinutes)*time.Minute, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) triageCmd(c core.Change) tea.Cmd {
	az := m.analyzer
	return func() tea.Msg { return adviceMsg(az.Triage(c)) }
}

// reviewCmd runs a read-only Claude review of the MR. When a local clone is
// configured, it reviews the MR branch in a throwaway worktree with full project
// context; otherwise it falls back to diff-only. The result is shown for
// confirmation before anything is posted.
func (m Model) reviewCmd(mr core.MR) tea.Cmd {
	rv, gl := m.reviewer, m.reviewGL
	prompt := m.cfg.ReviewPrompt
	if prompt == "" {
		prompt = config.DefaultReviewPrompt
	}
	opts := review.Options{
		ProjectsDir:  m.cfg.ProjectsDir,
		ProjectPaths: m.cfg.ProjectPaths,
		Worktree:     review.GitWorktree{},
		Skill:        m.cfg.ReviewSkill,
		PluginDirs:   m.cfg.PluginDirs,
	}
	return func() tea.Msg { return reviewMsg(review.Generate(gl, rv, mr, prompt, opts)) }
}

// postCmd posts a confirmed review as a comment. This is the only GitLab write.
func (m Model) postCmd(mr core.MR, body string) tea.Cmd {
	gl := m.reviewGL
	return func() tea.Msg {
		return postResultMsg{ref: mr.Ref, err: review.Post(gl, mr, body)}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// Keep the review viewport sized to the window if one is open, and
		// re-wrap its content to the new width.
		if m.pendingReview != nil {
			m.reviewVP.Width = m.reviewWidth()
			m.reviewVP.Height = m.reviewBodyHeight()
			m.setReviewContent(m.pendingReview.text)
		}
		return m, nil

	case tickMsg:
		// reschedule the metronome AND fetch; the two are independent
		return m, tea.Batch(m.tickCmd(), m.fetchCmd())

	case fetchErrMsg:
		m.status = "⚠ refresh failed: " + msg.err.Error()
		return m, nil

	case openErrMsg:
		m.status = "⚠ could not open browser: " + msg.err.Error()
		return m, nil

	case reviewMsg:
		m.reviewing = false
		res := review.Result(msg)
		if res.Err != nil {
			m.status = "⚠ review failed: " + res.Err.Error() + "  (full log: " + review.LogPath() + ")"
			return m, nil
		}
		// A configured review skill that did NOT actually fire means the run
		// degraded to an ad-hoc review (the skill flaked / Claude opted out).
		// Don't silently present that fallback — treat it as a failure the user
		// can retry, so they never unknowingly post a basic review instead of
		// the team's panel review.
		if m.cfg.ReviewSkill != "" && len(res.SkillsUsed) == 0 {
			m.status = "⚠ " + m.cfg.ReviewSkill + " did not run (likely transient) — press c to retry"
			return m, nil
		}
		// Stash the generated review and enter the confirm state; nothing is
		// posted until the user says yes.
		mr := m.byRef(res.Ref)
		if mr == nil {
			m.status = "⚠ review: MR no longer in list"
			return m, nil
		}
		m.pendingReview = &pending{ref: res.Ref, mr: *mr, text: res.Text}
		// Load the full review into a scrollable viewport so structured sections
		// (Blockers/Observations/…) past the first screen aren't truncated. The
		// content is word-wrapped to the viewport width — long lines wrap to the
		// next line rather than being clipped at the right edge.
		m.reviewVP = viewport.New(m.reviewWidth(), m.reviewBodyHeight())
		m.setReviewContent(res.Text)
		ctxNote := "diff-only"
		if res.LocalContext {
			ctxNote = "full project context"
		}
		if len(res.SkillsUsed) > 0 {
			ctxNote += fmt.Sprintf(", skill: %s ✓", strings.Join(res.SkillsUsed, ", "))
			if res.Subagents > 0 {
				ctxNote += fmt.Sprintf(", %d subagents", res.Subagents)
			}
		} else if m.cfg.ReviewSkill != "" {
			ctxNote += ", ⚠ skill not invoked"
		}
		m.status = "review ready (" + ctxNote + ") — post to MR? [y]es / [n]o"
		return m, nil

	case postResultMsg:
		if msg.err != nil {
			// Keep the pending review so the user can simply re-press y to retry
			// (likely a transient API blip). We don't auto-retry a POST: if the
			// failed attempt actually landed, a blind retry would duplicate the
			// comment. The full error goes to the log (the status bar truncates).
			review.Logf("post %s FAILED: %v", msg.ref, msg.err)
			m.status = "⚠ post failed (transient?) — [y] retry · [n]/esc discard · log: " + review.LogPath()
			return m, nil
		}
		m.pendingReview = nil
		review.Logf("post %s OK", msg.ref)
		m.status = "✓ review posted to " + msg.ref
		return m, nil

	case fetchResultMsg:
		res := watch.FetchResult(msg)
		if res.Err != nil {
			m.status = "⚠ refresh failed: " + res.Err.Error()
			return m, nil
		}
		m.applyMRs(res.MRs)
		m.loaded = true
		m.status = fmt.Sprintf("%d MRs · refreshed %s", len(res.MRs), time.Now().Format("15:04"))
		var cmds []tea.Cmd
		if m.autoTriage && m.analyzer != nil {
			for _, c := range watch.TriageWorthy(res.Changes) {
				cmds = append(cmds, m.triageCmd(c))
			}
		}
		return m, tea.Batch(cmds...)

	case adviceMsg:
		a := analyze.Advice(msg)
		if a.Err == nil && a.Text != "" {
			m.advice[a.Ref] = a.Text
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// applyMRs stores the full fetched list and recomputes the active-section view.
func (m *Model) applyMRs(all []core.MR) {
	m.allMRs = all
	m.refilter()
}

// refilter recomputes m.mrs for the active section from the already-fetched
// allMRs. It does NOT hit the network — sections are client-side filters over
// the same list, so switching tabs is instant.
func (m *Model) refilter() {
	filter := ""
	if m.sectionIdx < len(m.cfg.Sections) {
		filter = m.cfg.Sections[m.sectionIdx].Filter
	}
	matched := section.Filter(filter, m.allMRs)
	keysOrder, groups := section.GroupByTicket(matched)
	var flat []core.MR
	for _, k := range keysOrder {
		flat = append(flat, groups[k]...)
	}
	m.mrs = flat
	if m.cursor >= len(flat) {
		m.cursor = max(0, len(flat)-1)
	}
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Confirm state takes over the keyboard: a generated review awaits a
	// post/discard decision. Only ctrl+c escapes to quit.
	if m.pendingReview != nil {
		switch {
		case msg.Type == tea.KeyCtrlC:
			return m, tea.Quit
		case msg.String() == "y":
			p := m.pendingReview
			m.status = "posting review…"
			return m, m.postCmd(p.mr, p.text)
		case msg.String() == "n", msg.Type == tea.KeyEsc:
			m.pendingReview = nil
			m.status = "review discarded"
			return m, nil
		case msg.String() == "g":
			m.reviewVP.GotoTop()
			return m, nil
		case msg.String() == "G":
			m.reviewVP.GotoBottom()
			return m, nil
		}
		// Everything else scrolls the review (j/k, ↑/↓, PgUp/PgDn, u/d, f/b).
		var cmd tea.Cmd
		m.reviewVP, cmd = m.reviewVP.Update(msg)
		return m, cmd
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		return m, nil
	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.mrs)-1 {
			m.cursor++
		}
		return m, nil
	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case key.Matches(msg, m.keys.Top):
		m.cursor = 0
		return m, nil
	case key.Matches(msg, m.keys.Bottom):
		m.cursor = max(0, len(m.mrs)-1)
		return m, nil
	case key.Matches(msg, m.keys.NextSection):
		if len(m.cfg.Sections) > 0 {
			m.sectionIdx = (m.sectionIdx + 1) % len(m.cfg.Sections)
			m.cursor = 0
			m.refilter() // instant: re-filter the already-fetched list, no network
		}
		return m, nil
	case key.Matches(msg, m.keys.PrevSection):
		if n := len(m.cfg.Sections); n > 0 {
			m.sectionIdx = (m.sectionIdx - 1 + n) % n
			m.cursor = 0
			m.refilter()
		}
		return m, nil
	case key.Matches(msg, m.keys.Refresh):
		m.status = "refreshing…"
		return m, m.fetchCmd()
	case key.Matches(msg, m.keys.ToggleAuto):
		m.autoTriage = !m.autoTriage && m.analyzer != nil
		return m, nil
	case key.Matches(msg, m.keys.Expand):
		if mr := m.selected(); mr != nil {
			if m.expanded[mr.Ref] {
				delete(m.expanded, mr.Ref)
			} else {
				m.expanded[mr.Ref] = true
			}
		}
		return m, nil
	case key.Matches(msg, m.keys.Open):
		if mr := m.selected(); mr != nil {
			return m, openURL(mr.URL)
		}
		return m, nil
	case key.Matches(msg, m.keys.Triage):
		if mr := m.selected(); mr != nil && m.analyzer != nil {
			c := core.Change{Ref: mr.Ref, Title: mr.Title, Detail: "manual triage requested"}
			return m, m.triageCmd(c)
		}
		return m, nil
	case key.Matches(msg, m.keys.Review):
		if m.reviewer == nil || m.reviewGL == nil {
			m.status = "review unavailable (claude not found)"
			return m, nil
		}
		if mr := m.selected(); mr != nil && !m.reviewing {
			m.reviewing = true
			m.status = "reviewing " + mr.Ref + "…"
			return m, m.reviewCmd(*mr)
		}
		return m, nil
	}
	return m, nil
}

// byRef returns the MR with the given ref from the full fetched list, or nil.
func (m Model) byRef(ref string) *core.MR {
	for i := range m.allMRs {
		if m.allMRs[i].Ref == ref {
			return &m.allMRs[i]
		}
	}
	return nil
}

func (m Model) selected() *core.MR {
	if m.cursor >= 0 && m.cursor < len(m.mrs) {
		return &m.mrs[m.cursor]
	}
	return nil
}

// openURL launches the OS browser as a DETACHED background process. It uses a
// plain tea.Cmd (not tea.ExecProcess) so Bubble Tea keeps control of the screen
// — ExecProcess suspends the alt-screen and renderer for an interactive child,
// which for a fire-and-forget opener just causes a visible terminal flash.
func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		// Detach: the opener returns immediately and we don't want its stdio
		// touching our TTY. Start (not Run) so we never block the UI loop.
		if err := cmd.Start(); err != nil {
			return openErrMsg{err}
		}
		// Reap the short-lived process in the background so it isn't a zombie;
		// we don't care about its result.
		go func() { _ = cmd.Wait() }()
		return nil
	}
}

// setReviewContent word-wraps the review to the viewport width and loads it.
// lipgloss .Width(w) hard-wraps long lines to the next line so nothing is
// clipped at the right edge; the viewport then handles vertical scrolling.
func (m *Model) setReviewContent(text string) {
	wrapped := m.styles.Base.Width(m.reviewVP.Width).Render(text)
	m.reviewVP.SetContent(wrapped)
}

// reviewWidth/reviewBodyHeight size the scrollable review viewport, leaving room
// for the one-line header and the one-line hint (+1 blank line).
func (m Model) reviewWidth() int {
	if m.width < 1 {
		return 1
	}
	return m.width
}

func (m Model) reviewBodyHeight() int {
	h := m.height - 3 // header + blank + hint
	if h < 1 {
		return 1
	}
	return h
}

// reviewConfirmView shows the scrollable review plus a scroll indicator and the
// post/discard prompt. The full review is scrollable (j/k, ↑/↓, PgUp/PgDn) so
// structured sections past the first screen are reachable, not truncated.
func (m Model) reviewConfirmView() string {
	p := m.pendingReview
	pct := fmt.Sprintf("%3.0f%%", m.reviewVP.ScrollPercent()*100)
	header := m.styles.Header.Render("Claude review — "+p.ref) + "  " +
		m.styles.Help.Render(pct)
	hint := m.styles.Help.Render("scroll: j/k g/G PgUp/PgDn · [y] post to GitLab · [n]/esc discard")
	return strings.Join([]string{header, "", m.reviewVP.View(), hint}, "\n")
}

func (m Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	if m.showHelp {
		return m.help.FullHelpView(m.keys.FullHelp())
	}
	if m.pendingReview != nil {
		return m.reviewConfirmView()
	}

	// tabs, each with a count badge of how many MRs match it (once loaded).
	var tabs []string
	for i, s := range m.cfg.Sections {
		label := s.Title
		if m.loaded {
			label = fmt.Sprintf("%s (%d)", label, len(section.Filter(s.Filter, m.allMRs)))
		}
		if i == m.sectionIdx {
			label = m.styles.Header.Render("[" + label + "]")
		} else {
			label = m.styles.Footer.Render(" " + label + " ")
		}
		tabs = append(tabs, label)
	}
	tabBar := strings.Join(tabs, " ")

	// Full-width list. Each MR is one line, prefixed with a disclosure marker
	// (▸ collapsed / ▾ expanded). An expanded MR shows its detail indented
	// directly beneath. The statusline row width leaves room for the 2-col
	// marker prefix.
	rowWidth := m.width - 2
	if rowWidth < 1 {
		rowWidth = 1
	}
	var rows []string
	lastTicket := ""
	for i, mr := range m.mrs {
		if mr.TicketKey != lastTicket {
			rows = append(rows, m.styles.Header.Render(mr.TicketKey))
			lastTicket = mr.TicketKey
		}
		open := m.expanded[mr.Ref]
		marker := "▸ "
		if open {
			marker = "▾ "
		}
		if i == m.cursor {
			marker = m.styles.Accent.Render(marker)
		} else {
			marker = m.styles.Subtle.Render(marker)
		}
		rv := statusline.RowView{MR: mr, HasAdvice: m.advice[mr.Ref] != "", ApprovalsRequired: mr.ApprovalsRequired}
		rows = append(rows, marker+statusline.Render(m.cfg.Statusline, m.styles, rv, rowWidth, i == m.cursor))
		if open {
			rows = append(rows, detailpane.Render(m.styles, mr, m.advice[mr.Ref]))
		}
	}
	if len(rows) == 0 {
		if m.loaded {
			rows = append(rows, m.styles.Footer.Render("No matching MRs."))
		} else {
			rows = append(rows, m.styles.Footer.Render("loading…"))
		}
	}
	list := strings.Join(rows, "\n")

	auto := "OFF"
	if m.autoTriage {
		auto = "ON"
	}
	status := m.styles.Footer.Render(fmt.Sprintf("%s · auto-triage %s", m.status, auto))
	helpBar := m.help.ShortHelpView(m.keys.ShortHelp())

	// The body fills all vertical space between the tab bar and the footer so
	// the view occupies the full terminal height and the footer sits at the
	// bottom. Joining the four regions with "\n" yields exactly the sum of
	// their heights, so chrome is just the three single-line regions.
	chrome := lipgloss.Height(tabBar) + lipgloss.Height(status) + lipgloss.Height(helpBar)
	bodyHeight := m.height - chrome
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	body := lipgloss.NewStyle().Height(bodyHeight).Width(m.width).Render(list)

	return strings.Join([]string{tabBar, body, status, helpBar}, "\n")
}

