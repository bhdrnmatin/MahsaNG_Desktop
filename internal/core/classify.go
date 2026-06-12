package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"syscall"
	"time"

	"mahsang/internal/model"
)

// latencySamples is how many requests one ProbeDelay sends through the same
// instance to take a median latency. Building the xray instance dominates the
// cost, so extra samples through it are cheap; the median rides out the jitter
// that a single sample on a filtered uplink would mistake for a slow server.
const latencySamples = 4

// ProbeDelay is the classifying form of MeasureDelay: it runs the same
// YouTube-through-tunnel probe but, instead of folding every failure into one
// error, reports which censorship mechanism the failure points to. Feed the
// Verdicts (tagged with the user's ISP/region) into your own stats or OONI
// cross-checks to map how filtering behaves per network.
//
// It builds the instance once and samples latency several times through it,
// reporting the median so a working-but-jittery server is not condemned by one
// unlucky round trip.
func ProbeDelay(ctx context.Context, outboundJSON []byte, timeout time.Duration) model.ProbeResult {
	inst, err := startMeasureInstance(outboundJSON)
	if err != nil {
		// Build/start failure is about *this config*, not the network.
		return model.ProbeResult{Verdict: model.VerdictProxyError, PingMs: model.PingFailed, Detail: err.Error()}
	}
	defer inst.Close()

	client := &http.Client{Transport: proxyTransport(inst), Timeout: timeout}

	// The first sample also validates the body is genuinely the target (not a
	// block page); a failure here is the verdict for the whole probe.
	ms, verdict, detail := sampleDelay(ctx, client, true)
	if verdict != model.VerdictOK {
		return model.ProbeResult{Verdict: verdict, PingMs: model.PingFailed, Detail: detail}
	}

	samples := []int64{ms}
	for i := 1; i < latencySamples && ctx.Err() == nil; i++ {
		// Later samples only measure round-trip; a sporadic failure now is
		// jitter, not a verdict — the server already proved itself reachable.
		if m, v, _ := sampleDelay(ctx, client, false); v == model.VerdictOK {
			samples = append(samples, m)
		}
	}
	return model.ProbeResult{Verdict: model.VerdictOK, PingMs: medianMs(samples)}
}

// sampleDelay runs one probe request through client and times it to first
// response (headers in). The body is read only when validate is set, to detect
// a block page masquerading as 200; that read happens after the measurement so
// it cannot inflate it. On success it returns the latency and VerdictOK,
// otherwise the verdict the failure implies.
func sampleDelay(ctx context.Context, client *http.Client, validate bool) (int64, model.Verdict, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, measureURL, nil)
	if err != nil {
		return -1, model.VerdictProxyError, err.Error()
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return -1, classifyTransportErr(err), err.Error()
	}
	elapsed := time.Since(start)
	defer resp.Body.Close()

	// A non-200, or a 200 whose body is not really YouTube, is the censor
	// answering for the target rather than the target itself.
	if resp.StatusCode != http.StatusOK {
		return -1, model.VerdictBlockPage, fmt.Sprintf("http %d", resp.StatusCode)
	}
	if validate {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		if !bytes.Contains(bytes.ToLower(body), []byte(measureMarker)) {
			return -1, model.VerdictBlockPage, "marker absent (injected page)"
		}
	}
	return elapsed.Milliseconds(), model.VerdictOK, ""
}

// medianMs returns the median of samples (the upper-middle element for an even
// count). samples is non-empty by construction — the validated first sample is
// always present — and is reordered in place.
func medianMs(samples []int64) int64 {
	slices.Sort(samples)
	return samples[len(samples)/2]
}

// classifyTransportErr decides which mechanism a client.Do error implicates.
//
// Caveat worth knowing: the request egresses through xray's Dial, so a remote
// RST or drop is often wrapped in an xray error rather than surfacing as a clean
// syscall.Errno. We therefore check errors.Is for the typed cases AND fall back
// to substring matching on the message. The string fallback is the brittle part
// — if you want this to be reliable, the cleaner fix is to plumb a typed error
// out of proxyTransport's DialContext.
func classifyTransportErr(err error) model.Verdict {
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return model.VerdictTimeout
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return model.VerdictTimeout
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) {
		return model.VerdictReset
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "reset"), strings.Contains(msg, "refused"),
		strings.Contains(msg, "broken pipe"), strings.Contains(msg, "connection aborted"):
		return model.VerdictReset
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"),
		strings.Contains(msg, "i/o timeout"):
		return model.VerdictTimeout
	default:
		// No RST, no clear timeout: treat as a drop/black-hole (timeout-class).
		return model.VerdictTimeout
	}
}
