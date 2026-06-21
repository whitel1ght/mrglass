package section

import (
	"sort"

	"github.com/expr-lang/expr"
	"github.com/dmitry/mrglass/internal/core"
)

func env(mr core.MR, approvalsRequired int) map[string]any {
	return map[string]any{
		"role":       mr.Role.String(),
		"ci":         mr.CI,
		"draft":      mr.Draft,
		"conflicts":  mr.Conflicts,
		"unresolved": mr.Unresolved,
		"comments":   mr.Comments,
		"approvedBy": mr.ApprovedBy,
		"required":   approvalsRequired,
		"author":     mr.Author,
		"title":      mr.Title,
	}
}

// Match evaluates a filter predicate against an MR. A broken filter matches nothing.
func Match(filter string, mr core.MR, approvalsRequired int) bool {
	e := env(mr, approvalsRequired)
	prog, err := expr.Compile(filter, expr.Env(e), expr.AsBool())
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
		if Match(filter, mr, 0) {
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
