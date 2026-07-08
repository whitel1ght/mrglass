package provider

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/whitel1ght/mrglass/internal/core"
)

func TestEnrichAllEnrichesEveryMR(t *testing.T) {
	found := map[string]core.MR{
		"a": {Ref: "a"}, "b": {Ref: "b"}, "c": {Ref: "c"},
	}
	var mu sync.Mutex
	seen := map[string]bool{}
	out := EnrichAll(found, 4, func(mr core.MR) core.MR {
		mu.Lock()
		seen[mr.Ref] = true
		mu.Unlock()
		mr.Title = "enriched"
		return mr
	})
	if len(out) != 3 || len(seen) != 3 {
		t.Fatalf("want all 3 enriched, got out=%d seen=%d", len(out), len(seen))
	}
	for _, mr := range out {
		if mr.Title != "enriched" {
			t.Errorf("%s: enrich result not kept", mr.Ref)
		}
	}
}

func TestEnrichAllBoundsConcurrency(t *testing.T) {
	found := map[string]core.MR{}
	for _, r := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		found[r] = core.MR{Ref: r}
	}
	var inFlight, peak atomic.Int32
	gate := make(chan struct{})
	out := make(chan []core.MR, 1)
	go func() {
		out <- EnrichAll(found, 2, func(mr core.MR) core.MR {
			n := inFlight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			<-gate
			inFlight.Add(-1)
			return mr
		})
	}()
	close(gate) // release everyone; peak was recorded on entry
	if got := <-out; len(got) != 8 {
		t.Fatalf("want 8 results, got %d", len(got))
	}
	if p := peak.Load(); p > 2 {
		t.Errorf("concurrency peaked at %d, limit was 2", p)
	}
}

func TestEnrichAllEmpty(t *testing.T) {
	if out := EnrichAll(nil, 4, func(mr core.MR) core.MR { return mr }); len(out) != 0 {
		t.Errorf("nil input should give empty output, got %v", out)
	}
}
