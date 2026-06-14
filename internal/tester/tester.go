// Package tester measures real-ping latency for many configs concurrently,
// mirroring the Android app's fixed thread pool of 10 in V2RayTestService.
package tester

import (
	"context"
	"net"
	"sort"
	"sync"
	"sync/atomic"
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
	Index   int
	PingMs  int64
	Verdict model.Verdict
	// Fragment is whether the auto-retry found this server needs TLS-ClientHello
	// fragmentation, so the UI can flag it.
	Fragment bool
}

// probe measures one config and classifies the outcome (which censorship
// mechanism a failure points to), optionally fragmenting the TLS ClientHello. A
// variable so tests can stub out the real xray-core round trip.
var probe = func(ctx context.Context, outbound []byte, fragment bool) model.ProbeResult {
	return core.ProbeDelay(ctx, outbound, probeTimeout, fragment)
}

// TestAll measures configs' latency using a worker pool. It writes the result
// back into each config's PingMs and, if onResult is non-nil, calls it as each
// measurement completes. Respects ctx cancellation.
//
// When wantGood > 0 it stops feeding new probes once that many servers come back
// working — the user just needs the fastest few live servers, not a full ranking
// of the dead tail (each dead-but-accepting config otherwise burns the whole
// probeTimeout). Configs are probed previously-good-first so those targets are
// reached soonest; the configs never reached keep their prior PingMs. Pass
// wantGood <= 0 to probe everything.
func TestAll(ctx context.Context, configs []model.Config, wantGood int, onResult func(Result)) {
	jobs := make(chan int)
	var wg sync.WaitGroup
	var goodCount int64

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				res := probe(ctx, configs[i].Outbound, configs[i].Fragment)
				// One failed sample on a server that worked before is
				// usually probe noise (overload, probabilistic filtering),
				// not death: give previously-good servers a second chance
				// before flipping them to failed.
				if res.PingMs == model.PingFailed && configs[i].PingMs >= 0 && ctx.Err() == nil {
					res = probe(ctx, configs[i].Outbound, configs[i].Fragment)
				}
				// A reset is SNI-based DPI tearing down the TLS handshake.
				// Fragmenting the ClientHello often defeats it, so retry once
				// with fragmentation; if that works, remember this server needs
				// it (for later probes and the live tunnel).
				if res.Verdict == model.VerdictReset && !configs[i].Fragment && ctx.Err() == nil {
					if fr := probe(ctx, configs[i].Outbound, true); fr.Verdict == model.VerdictOK {
						res = fr
						configs[i].Fragment = true
					}
				}
				configs[i].PingMs = res.PingMs
				configs[i].LastVerdict = res.Verdict
				if res.Verdict == model.VerdictOK {
					atomic.AddInt64(&goodCount, 1)
				}
				if onResult != nil {
					onResult(Result{Index: i, PingMs: res.PingMs, Verdict: res.Verdict, Fragment: configs[i].Fragment})
				}
			}
		}()
	}

	for _, i := range probeOrder(configs) {
		if wantGood > 0 && atomic.LoadInt64(&goodCount) >= int64(wantGood) {
			break
		}
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

// probeOrder returns config indices ordered so an early-stopping TestAll reaches
// working servers soonest: previously-good first (fastest first), then untested,
// then previously-failed last. A previously-good server is the best bet to pass
// again quickly, so probing those first lets wantGood fill before the slow dead
// configs are ever touched.
func probeOrder(configs []model.Config) []int {
	order := make([]int, len(configs))
	for i := range order {
		order[i] = i
	}
	rank := func(p int64) int {
		switch {
		case p >= 0:
			return 0 // previously good
		case p == model.PingUntested:
			return 1
		default: // PingFailed
			return 2
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		pa, pb := configs[order[a]].PingMs, configs[order[b]].PingMs
		if ra, rb := rank(pa), rank(pb); ra != rb {
			return ra < rb
		} else if ra == 0 {
			return pa < pb // fastest known-good first
		}
		return false
	})
	return order
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
				// Serverless configs have no single server to dial — they are
				// always "reachable" in this sense; the real test is the probe.
				if configs[i].IsServerless() {
					alive[i] = true
					continue
				}
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
