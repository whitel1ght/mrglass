package tui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dmitry/mrglass/internal/analyze"
	"github.com/dmitry/mrglass/internal/config"
	"github.com/dmitry/mrglass/internal/core"
	"github.com/dmitry/mrglass/internal/provider"
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

type Model struct {
	cfg       config.Config
	provider  provider.Provider
	me        string
	analyzer  analyze.Analyzer // may be nil
	statePath string

	keys   keys.KeyMap
	help   help.Model
	styles theme.Styles

	allMRs     []core.MR // full fetched list (provider-agnostic, unfiltered)
	mrs        []core.MR // current active-section rows (flattened, grouped order)
	cursor     int
	sectionIdx int
	loaded     bool // true once the first fetch has returned
	advice     map[string]string

	autoTriage bool
	status     string
	showHelp   bool
	width      int
	height     int
}

func New(cfg config.Config, p provider.Provider, me string, az analyze.Analyzer, statePath string) Model {
	return Model{
		cfg: cfg, provider: p, me: me, analyzer: az, statePath: statePath,
		keys: keys.Default(), help: help.New(),
		styles:     theme.BuildStyles(theme.Get(cfg.Theme)),
		advice:     map[string]string{},
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

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
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
	}
	return m, nil
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

func (m Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	if m.showHelp {
		return m.help.FullHelpView(m.keys.FullHelp())
	}

	listWidth := m.width * 6 / 10
	detailWidth := m.width - listWidth - 1

	// tabs
	var tabs []string
	for i, s := range m.cfg.Sections {
		label := s.Title
		if i == m.sectionIdx {
			label = m.styles.Header.Render("[" + label + "]")
		} else {
			label = m.styles.Footer.Render(" " + label + " ")
		}
		tabs = append(tabs, label)
	}
	tabBar := strings.Join(tabs, " ")

	// list
	var rows []string
	lastTicket := ""
	for i, mr := range m.mrs {
		if mr.TicketKey != lastTicket {
			rows = append(rows, m.styles.Header.Render(mr.TicketKey))
			lastTicket = mr.TicketKey
		}
		rv := statusline.RowView{MR: mr, HasAdvice: m.advice[mr.Ref] != "", ApprovalsRequired: mr.ApprovalsRequired}
		rows = append(rows, statusline.Render(m.cfg.Statusline, m.styles, rv, listWidth, i == m.cursor))
	}
	if len(rows) == 0 {
		if m.loaded {
			rows = append(rows, m.styles.Footer.Render("No matching MRs."))
		} else {
			rows = append(rows, m.styles.Footer.Render("loading…"))
		}
	}
	list := strings.Join(rows, "\n")

	// detail
	detail := ""
	if mr := m.selected(); mr != nil {
		detail = detailpane.Render(m.styles, *mr, m.advice[mr.Ref], detailWidth)
	}

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

	pane := lipgloss.NewStyle().Height(bodyHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		pane.Width(listWidth).Render(list),
		pane.Width(detailWidth).Render(detail),
	)

	return strings.Join([]string{tabBar, body, status, helpBar}, "\n")
}

