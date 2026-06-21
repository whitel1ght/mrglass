package core

import (
	"fmt"
	"sort"
	"strings"
)

type ChangeKind int

const (
	KindNew ChangeKind = iota
	KindGone
	KindChanged
)

func (k ChangeKind) String() string {
	switch k {
	case KindNew:
		return "new"
	case KindGone:
		return "gone"
	case KindChanged:
		return "changed"
	default:
		return "unknown"
	}
}

type FieldChange struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

type Change struct {
	Ref    string        `json:"ref"`
	URL    string        `json:"url"`
	Title  string        `json:"title"`
	Kind   ChangeKind    `json:"kind"`
	Detail string        `json:"detail"`
	Fields []FieldChange `json:"fields"`
}

// Diff compares two {ref: Snapshot} maps and returns meaningful transitions
// only, sorted by Ref. Cosmetic churn is excluded by Snapshot's field set.
func Diff(prev, curr map[string]Snapshot) []Change {
	var changes []Change

	var newRefs, goneRefs, common []string
	for ref := range curr {
		if _, ok := prev[ref]; ok {
			common = append(common, ref)
		} else {
			newRefs = append(newRefs, ref)
		}
	}
	for ref := range prev {
		if _, ok := curr[ref]; !ok {
			goneRefs = append(goneRefs, ref)
		}
	}
	sort.Strings(newRefs)
	sort.Strings(goneRefs)
	sort.Strings(common)

	for _, ref := range newRefs {
		c := curr[ref]
		changes = append(changes, Change{
			Ref: ref, URL: c.URL, Title: c.Title, Kind: KindNew,
			Detail: "appeared (new/now in scope)",
		})
	}
	for _, ref := range goneRefs {
		p := prev[ref]
		changes = append(changes, Change{
			Ref: ref, URL: p.URL, Title: p.Title, Kind: KindGone,
			Detail: "left scope (merged/closed/aged out)",
		})
	}
	for _, ref := range common {
		a, b := prev[ref], curr[ref]
		fields, details := diffFields(a, b)
		if len(details) > 0 {
			changes = append(changes, Change{
				Ref: ref, URL: b.URL, Title: b.Title, Kind: KindChanged,
				Detail: strings.Join(details, "; "), Fields: fields,
			})
		}
	}
	return changes
}

func diffFields(a, b Snapshot) ([]FieldChange, []string) {
	var fields []FieldChange
	var details []string

	if a.CI != b.CI {
		fields = append(fields, FieldChange{"ci", a.CI, b.CI})
		details = append(details, fmt.Sprintf("CI %s → %s", dash(a.CI), dash(b.CI)))
	}
	if !sameSet(a.ApprovedBy, b.ApprovedBy) {
		fields = append(fields, FieldChange{"approved_by", a.ApprovedBy, b.ApprovedBy})
		gained := diffSet(b.ApprovedBy, a.ApprovedBy)
		lost := diffSet(a.ApprovedBy, b.ApprovedBy)
		if len(gained) > 0 {
			details = append(details, "approved by "+strings.Join(gained, ", "))
		}
		if len(lost) > 0 {
			details = append(details, "approval removed by "+strings.Join(lost, ", "))
		}
	}
	if a.Conflicts != b.Conflicts {
		fields = append(fields, FieldChange{"conflicts", a.Conflicts, b.Conflicts})
		if b.Conflicts {
			details = append(details, "conflicts appeared")
		} else {
			details = append(details, "conflicts resolved")
		}
	}
	if a.Unresolved != b.Unresolved {
		fields = append(fields, FieldChange{"unresolved", a.Unresolved, b.Unresolved})
		if b.Unresolved {
			details = append(details, "new unresolved threads")
		} else {
			details = append(details, "threads resolved")
		}
	}
	if b.Comments > a.Comments {
		fields = append(fields, FieldChange{"comments", a.Comments, b.Comments})
		details = append(details, fmt.Sprintf("+%d comment(s)", b.Comments-a.Comments))
	}
	if a.Draft != b.Draft {
		fields = append(fields, FieldChange{"draft", a.Draft, b.Draft})
		if b.Draft {
			details = append(details, "marked draft")
		} else {
			details = append(details, "marked ready")
		}
	}
	return fields, details
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]bool{}
	for _, x := range a {
		m[x] = true
	}
	for _, x := range b {
		if !m[x] {
			return false
		}
	}
	return true
}

// diffSet returns elements in a not in b, sorted.
func diffSet(a, b []string) []string {
	in := map[string]bool{}
	for _, x := range b {
		in[x] = true
	}
	var out []string
	for _, x := range a {
		if !in[x] {
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}
