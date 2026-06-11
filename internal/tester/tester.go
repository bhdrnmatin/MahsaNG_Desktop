// Package tester measures real-ping latency for many configs concurrently,
// mirroring the Android app's fixed thread pool of 10 in V2RayTestService.
package tester

import (
	"context"
	"net"
	"sort"
	"sync"
	"time"

	"mahsang/internal/core"
	"mahsang/internal/model"
)

const (
	// Each test is almost entirely network wait, but too much concurrency
	// saturates a slow/filtered uplink and pushes live servers into timeout
	// (measured: 40 workers -> ~everything fails, sequential -> some pass).
	// 16 keeps a 100-config batch reasonably fast without that distortion.
	workers = 16
	// Dead/unreachable configs are the main time sink: they block until this
	// timeout. On filtered networks live servers can take several seconds to
	// complete the probe, so this errs long rather than marking them dead.
	probeTimeout = 10 * time.Second
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
					ping = model.PingFailed
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

const (
	// A TCP dial is far cheaper than a full proxy probe (no xray instance, no
	// TLS), so reachability checks can run much wider and give up sooner.
	dialWorkers = 256
	dialTimeout = 3 * time.Second
)

// FilterAlive returns the configs whose server accepts a TCP connection,
// preserving input order. Most dead free configs don't even accept the
// connection, so this cheap pre-filter lets GET CONFIG sift several times
// more candidates before the expensive full probe.
func FilterAlive(ctx context.Context, configs []model.Config) []model.Config {
	alive := make([]bool, len(configs))
	jobs := make(chan int)
	dialer := &net.Dialer{Timeout: dialTimeout}
	var wg sync.WaitGroup

	for w := 0; w < dialWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				addr, err := core.OutboundHostPort(configs[i].Outbound)
				if err != nil {
					continue
				}
				conn, err := dialer.DialContext(ctx, "tcp", addr)
				if err == nil {
					conn.Close()
					alive[i] = true
				}
			}
		}()
	}

feed:
	for i := range configs {
		select {
		case <-ctx.Done():
			break feed
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()

	out := make([]model.Config, 0, len(configs))
	for i, ok := range alive {
		if ok {
			out = append(out, configs[i])
		}
	}
	return out
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
