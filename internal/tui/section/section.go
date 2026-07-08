package section

import (
	"sort"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/whitel1ght/mrglass/internal/core"
)

func env(mr core.MR) map[string]any {
	return map[string]any{
		"role":       mr.Role.String(),
		"ci":         mr.CI,
		"draft":      mr.Draft,
		"conflicts":  mr.Conflicts,
		"unresolved": mr.Unresolved,
		"comments":   mr.Comments,
		"approvedBy": mr.ApprovedBy,
		"required":   mr.ApprovalsRequired,
		"author":     mr.Author,
		"title":      mr.Title,
	}
}

// progs caches compiled filter programs; filters are fixed config strings
// evaluated per MR per render, so compile each once.
var progs sync.Map // filter string -> *vm.Program

func compile(filter string, e map[string]any) (*vm.Program, error) {
	if v, ok := progs.Load(filter); ok {
		return v.(*vm.Program), nil
	}
	prog, err := expr.Compile(filter, expr.Env(e), expr.AsBool())
	if err != nil {
		return nil, err
	}
	progs.Store(filter, prog)
	return prog, nil
}

// Match evaluates a filter predicate against an MR. A broken filter matches nothing.
func Match(filter string, mr core.MR) bool {
	e := env(mr)
	prog, err := compile(filter, e)
	if err != nil {
		return false
	}
	out, err := expr.Run(prog, e)
	if err != nil {
		return false
	}
	b, _ := out.(bool)
	return b
}

// Filter returns the MRs matching the predicate, preserving order.
func Filter(filter string, mrs []core.MR) []core.MR {
	var out []core.MR
	for _, mr := range mrs {
		if Match(filter, mr) {
			out = append(out, mr)
		}
	}
	return out
}

// GroupByTicket groups MRs by TicketKey; keys are sorted with "Other" last.
func GroupByTicket(mrs []core.MR) ([]string, map[string][]core.MR) {
	groups := map[string][]core.MR{}
	for _, mr := range mrs {
		k := mr.TicketKey
		if k == "" {
			k = "Other"
		}
		groups[k] = append(groups[k], mr)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		oi, oj := keys[i] == "Other", keys[j] == "Other"
		if oi != oj {
			return oj // non-Other first
		}
		return keys[i] < keys[j]
	})
	return keys, groups
}
