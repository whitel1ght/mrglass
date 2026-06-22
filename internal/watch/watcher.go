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
	Err     error
}

type Deps struct {
	Provider  provider.Provider
	Me        string
	StatePath string
	Cfg       config.Config
}

// Fetch lists MRs, diffs against saved state, persists the new state, notifies on
// each change, and returns the MRs plus the change-list.
func Fetch(d Deps) FetchResult {
	mrs, err := d.Provider.List(d.Me, d.Cfg.Days, d.Cfg.TicketRegex)
	if err != nil {
		return FetchResult{Err: err}
	}
	prev, _ := core.LoadState(d.StatePath)
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
	_ = core.SaveState(d.StatePath, curr)
	for _, c := range changes {
		Notify(c)
	}
	return FetchResult{MRs: mrs, Changes: changes}
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
