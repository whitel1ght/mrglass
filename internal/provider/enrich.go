package provider

import (
	"sync"

	"github.com/whitel1ght/mrglass/internal/core"
)

// EnrichAll runs enrich over every MR with at most limit calls in flight —
// the per-MR detail fetch is each provider's slow path, and 3-4 concurrent
// calls stays polite to the forge API. Result order is unspecified (callers
// previously iterated a map). Each result index is written by exactly one
// goroutine.
func EnrichAll(found map[string]core.MR, limit int, enrich func(core.MR) core.MR) []core.MR {
	result := make([]core.MR, len(found))
	var wg sync.WaitGroup
	sem := make(chan struct{}, limit)
	i := 0
	for _, mr := range found {
		wg.Add(1)
		go func(idx int, mr core.MR) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			result[idx] = enrich(mr)
		}(i, mr)
		i++
	}
	wg.Wait()
	return result
}
