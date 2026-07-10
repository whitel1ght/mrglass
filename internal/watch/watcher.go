package watch

import (
	"github.com/whitel1ght/mrglass/internal/analyze"
	"github.com/whitel1ght/mrglass/internal/config"
	"github.com/whitel1ght/mrglass/internal/core"
	"github.com/whitel1ght/mrglass/internal/provider"
)

type FetchResult struct {
	MRs     []core.MR
	Changes []core.Change
	Warning string // non-fatal problem (state load/save); dashboard still works
	Err     error
}

type Deps struct {
	Provider  provider.Provider
	Me        string
	StatePath string
	Cfg       config.Config
	Hidden    map[string]bool // user-hidden refs: their changes are fully muted
}

// Fetch lists MRs, diffs against saved state, persists the new state, notifies on
// each change, and returns the MRs plus the change-list.
func Fetch(d Deps) FetchResult {
	mrs, err := d.Provider.List(d.Me, d.Cfg.Days, d.Cfg.TicketRegex)
	if err != nil {
		return FetchResult{Err: err}
	}
	prev, loadErr := core.LoadState(d.StatePath)
	curr := map[string]core.Snapshot{}
	for _, m := range mrs {
		p := prev[m.Ref]
		var pp *core.Snapshot
		if _, ok := prev[m.Ref]; ok {
			pp = &p
		}
		curr[m.Ref] = core.Snap(m, pp)
	}
	var changes []core.Change
	if len(prev) > 0 {
		changes = core.Diff(prev, curr)
	}
	// Hidden MRs are fully muted: their changes neither notify nor reach the
	// TUI's auto-triage. Snapshots above still track them, so unhiding later
	// never replays a backlog of stale notifications.
	if len(d.Hidden) > 0 {
		kept := changes[:0]
		for _, c := range changes {
			if !d.Hidden[c.Ref] {
				kept = append(kept, c)
			}
		}
		changes = kept
	}
	var warning string
	if loadErr != nil {
		warning = loadErr.Error()
	}
	if err := core.SaveState(d.StatePath, curr); err != nil {
		// Without persisted state every refresh looks like a first run and
		// change detection silently dies — tell the user.
		warning = "state save failed: " + err.Error()
	}
	for _, c := range changes {
		Notify(c)
	}
	return FetchResult{MRs: mrs, Changes: changes, Warning: warning}
}

// TriageWorthy filters changes down to those worth a Claude call.
func TriageWorthy(changes []core.Change) []core.Change {
	var out []core.Change
	for _, c := range changes {
		if analyze.IsTriageWorthy(c) {
			out = append(out, c)
		}
	}
	return out
}
