// Package tester measures real-ping latency for many configs concurrently,
// mirroring the Android app's fixed thread pool of 10 in V2RayTestService.
package tester

import (
	"context"
	"sort"
	"sync"
	"time"

	"mahsang/internal/core"
	"mahsang/internal/model"
)

const (
	workers       = 10
	probeTimeout  = 10 * time.Second
)

// Result reports the outcome of testing one config (by its index in the slice
// passed to TestAll), so the UI can update that row live.
type Result struct {
	Index  int
	PingMs int64
}

// TestAll measures every config's latency using a worker pool. It writes the
// result back into each config's PingMs and, if onResult is non-nil, calls it
// as each measurement completes. Respects ctx cancellation.
func TestAll(ctx context.Context, configs []model.Config, onResult func(Result)) {
	jobs := make(chan int)
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				ping, err := core.MeasureDelay(ctx, configs[i].Outbound, probeTimeout)
				if err != nil {
					ping = -1
				}
				configs[i].PingMs = ping
				if onResult != nil {
					onResult(Result{Index: i, PingMs: ping})
				}
			}
		}()
	}

	for i := range configs {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()
}

// SortByPing orders configs fastest-first; untested/failed (-1) sink to the end.
func SortByPing(configs []model.Config) {
	sort.SliceStable(configs, func(i, j int) bool {
		a, b := configs[i].PingMs, configs[j].PingMs
		if a < 0 {
			return false
		}
		if b < 0 {
			return true
		}
		return a < b
	})
}
