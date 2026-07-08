package provider

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	const limit = 2
	var inFlight, peak atomic.Int32
	gate := make(chan struct{})
	// Buffered for every MR, not just `limit`: every enrich call signals
	// entry, and once `gate` closes all 8 goroutines run enrich and send
	// here. A cap of `limit` would let goroutines beyond the first two
	// block on this send forever, since the test only ever drains `limit`
	// signals.
	started := make(chan struct{}, len(found))
	out := make(chan []core.MR, 1)
	go func() {
		out <- EnrichAll(found, limit, func(mr core.MR) core.MR {
			n := inFlight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			started <- struct{}{}
			<-gate
			inFlight.Add(-1)
			return mr
		})
	}()
	// Wait for exactly `limit` concurrent calls to have entered before
	// releasing the gate, so peak is deterministically driven to the
	// bound rather than depending on scheduler timing. A serialized
	// implementation can never deliver the second signal (its lone
	// goroutine is parked on <-gate before a second one starts), so this
	// times out instead of hanging forever.
	for i := 0; i < limit; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for %d concurrent enrich calls to start (got %d)", limit, i)
		}
	}
	close(gate) // release everyone; peak was recorded on entry
	if got := <-out; len(got) != 8 {
		t.Fatalf("want 8 results, got %d", len(got))
	}
	if p := peak.Load(); p != 2 {
		t.Errorf("concurrency peaked at %d, want exactly %d", p, limit)
	}
}

func TestEnrichAllEmpty(t *testing.T) {
	if out := EnrichAll(nil, 4, func(mr core.MR) core.MR { return mr }); len(out) != 0 {
		t.Errorf("nil input should give empty output, got %v", out)
	}
}
