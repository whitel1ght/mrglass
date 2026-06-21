// Package analyze turns detected MR changes into short human advice via Claude,
// gated by a pure-Go pre-filter so tokens are spent only when warranted.
package analyze

import "github.com/dmitry/mrglass/internal/core"

// Advice is the result of triaging one change.
type Advice struct {
	Ref  string
	Text string
	Err  error
}

// Analyzer produces advice for a meaningful change.
type Analyzer interface {
	Triage(c core.Change) Advice
}

// IsTriageWorthy decides — without any tokens — whether a change merits a Claude
// call. Only actionable problems qualify; informational changes notify only.
func IsTriageWorthy(c core.Change) bool {
	if c.Kind != core.KindChanged {
		return false
	}
	for _, f := range c.Fields {
		switch f.Field {
		case "ci":
			if s, ok := f.New.(string); ok && s == "failed" {
				return true
			}
		case "conflicts":
			if b, ok := f.New.(bool); ok && b {
				return true
			}
		case "unresolved":
			if b, ok := f.New.(bool); ok && b {
				return true
			}
		case "approved_by":
			if approvalLost(f) {
				return true
			}
		}
	}
	return false
}

func approvalLost(f core.FieldChange) bool {
	old, ok1 := f.Old.([]string)
	nw, ok2 := f.New.([]string)
	if !ok1 || !ok2 {
		return false
	}
	have := map[string]bool{}
	for _, x := range nw {
		have[x] = true
	}
	for _, x := range old {
		if !have[x] {
			return true
		}
	}
	return false
}
