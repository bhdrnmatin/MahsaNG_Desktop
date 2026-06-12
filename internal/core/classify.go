package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"syscall"
	"time"

	"mahsang/internal/model"
)

// ProbeDelay is the classifying form of MeasureDelay: it runs the same
// YouTube-through-tunnel probe but, instead of folding every failure into one
// error, reports which censorship mechanism the failure points to. Feed the
// Verdicts (tagged with the user's ISP/region) into your own stats or OONI
// cross-checks to map how filtering behaves per network.
func ProbeDelay(ctx context.Context, outboundJSON []byte, timeout time.Duration) model.ProbeResult {
	inst, err := startMeasureInstance(outboundJSON)
	if err != nil {
		// Build/start failure is about *this config*, not the network.
		return model.ProbeResult{Verdict: model.VerdictProxyError, PingMs: model.PingFailed, Detail: err.Error()}
	}
	defer inst.Close()

	client := &http.Client{Transport: proxyTransport(inst), Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, measureURL, nil)
	if err != nil {
		return model.ProbeResult{Verdict: model.VerdictProxyError, PingMs: model.PingFailed, Detail: err.Error()}
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return model.ProbeResult{Verdict: classifyTransportErr(err), PingMs: model.PingFailed, Detail: err.Error()}
	}
	elapsed := time.Since(start)
	defer resp.Body.Close()

	// A non-200, or a 200 whose body is not really YouTube, is the censor
	// answering for the target rather than the target itself.
	if resp.StatusCode != http.StatusOK {
		return model.ProbeResult{Verdict: model.VerdictBlockPage, PingMs: model.PingFailed, Detail: fmt.Sprintf("http %d", resp.StatusCode)}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if !bytes.Contains(bytes.ToLower(body), []byte(measureMarker)) {
		return model.ProbeResult{Verdict: model.VerdictBlockPage, PingMs: model.PingFailed, Detail: "marker absent (injected page)"}
	}
	return model.ProbeResult{Verdict: model.VerdictOK, PingMs: elapsed.Milliseconds()}
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
