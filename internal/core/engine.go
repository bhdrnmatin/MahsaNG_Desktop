// Package core wraps xray-core: it builds short-lived instances to measure a
// server's real latency, and long-lived instances that expose a local SOCKS
// proxy to route traffic through a chosen server.
//
// xray-core is itself written in Go, so we embed it directly as a library
// rather than shelling out to a binary. This mirrors what the Android app does
// through its libv2ray wrapper (Libv2ray.measureOutboundDelay).
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"time"

	xcore "github.com/xtls/xray-core/core"

	// Register all inbound/outbound protocols and transports (vmess, vless,
	// trojan, shadowsocks, tls, ws, grpc, reality, ...). Without this the
	// config loader cannot build the protobuf for these features.
	_ "github.com/xtls/xray-core/main/distro/all"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/infra/conf/serial"
)

// measureURL is the latency probe target: a site that is filtered in Iran.
// Reaching it proves the tunnel egresses *outside* the national filter, not
// merely that the server accepts a connection — an unfiltered target like
// Google answers even through a relay that is itself still behind the filter.
// A var (not const) only so tests can point the probe at a local server.
var measureURL = "https://www.youtube.com/"

// measureMarker is a substring present in a genuine YouTube response but not in
// the (Persian) national block page that the censor injects with HTTP 200 for
// filtered sites. Finding it is what distinguishes a real egress from a block
// page masquerading as success.
const measureMarker = "youtube"

// speedBytes is how much we ask the speed-test endpoint to send (10 MB). The
// download stops early if the time window elapses first.
const speedBytes = 10 << 20

// speedURLs are throughput-probe targets, tried in order. The first streams an
// arbitrary byte count from Cloudflare's edge; the second is a static 10 MB
// file. Some servers reset/block a particular host, so a fallback improves the
// odds of getting a reading.
var speedURLs = []string{
	fmt.Sprintf("https://speed.cloudflare.com/__down?bytes=%d", speedBytes),
	"http://cachefly.cachefly.net/10mb.test",
}

// proxyTransport returns an http.Transport whose connections are dialed through
// the given xray instance, so requests made with it egress via the proxy.
func proxyTransport(inst *xcore.Instance) *http.Transport {
	return &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dest, err := xnet.ParseDestination(network + ":" + addr)
			if err != nil {
				return nil, err
			}
			return xcore.Dial(ctx, inst, dest)
		},
	}
}

// buildInstance creates (but does not start) an xray instance whose primary
// outbound is the given outbound JSON object. extra is merged at the top level
// of the config (used to add a SOCKS inbound for the live tunnel). When fragment
// is set, the proxy outbound dials through a TLS-ClientHello fragmenting outbound
// so SNI-based DPI cannot reassemble the server name.
func buildInstance(outboundJSON []byte, fragment bool, extra map[string]any) (*xcore.Instance, error) {
	proxy := mustObject(outboundJSON)
	outbounds := []any{proxy}
	// Skip injection if the outbound isn't a JSON object; the loader will then
	// fail with a clear error, same as the non-fragment path.
	if pm, ok := proxy.(map[string]any); ok && fragment {
		setDialerProxy(pm, "fragment")
		outbounds = append(outbounds, fragmentOutbound())
	}
	full := map[string]any{
		"log":       map[string]any{"loglevel": "none"},
		"outbounds": outbounds,
	}
	maps.Copy(full, extra)
	cfgBytes, err := json.Marshal(full)
	if err != nil {
		return nil, err
	}
	pbConfig, err := serial.LoadJSONConfig(bytes.NewReader(cfgBytes))
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	inst, err := xcore.New(pbConfig)
	if err != nil {
		return nil, fmt.Errorf("new instance: %w", err)
	}
	return inst, nil
}

// fragmentOutbound is a freedom outbound that splits the outgoing TLS
// ClientHello across packets, so DPI reading the SNI cannot reassemble it. The
// proxy outbound routes its dial through this via sockopt.dialerProxy. The
// length/interval ranges are the values MahsaNG / v2rayNG ship by default.
func fragmentOutbound() map[string]any {
	return map[string]any{
		"tag":      "fragment",
		"protocol": "freedom",
		"settings": map[string]any{
			"fragment": map[string]any{
				"packets":  "tlshello",
				"length":   "100-200",
				"interval": "10-20",
			},
		},
	}
}

// setDialerProxy makes ob dial through the outbound with the given tag, creating
// the streamSettings.sockopt path if it is absent.
func setDialerProxy(ob map[string]any, tag string) {
	ss, _ := ob["streamSettings"].(map[string]any)
	if ss == nil {
		ss = map[string]any{}
		ob["streamSettings"] = ss
	}
	sockopt, _ := ss["sockopt"].(map[string]any)
	if sockopt == nil {
		sockopt = map[string]any{}
		ss["sockopt"] = sockopt
	}
	sockopt["dialerProxy"] = tag
}

// startMeasureInstance builds and starts a throwaway instance for the given
// outbound, used by the delay and speed probes. The caller must Close it.
func startMeasureInstance(outboundJSON []byte, fragment bool) (*xcore.Instance, error) {
	inst, err := buildInstance(outboundJSON, fragment, nil)
	if err != nil {
		return nil, err
	}
	if err := inst.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	return inst, nil
}

// Tunnel is a running xray instance exposing a local SOCKS5 and HTTP proxy that
// routes traffic through the chosen server. Apps/browser point at
// 127.0.0.1:SocksPort (or HTTPPort); the system proxy and the TUN both feed off
// these local listeners.
type Tunnel struct {
	inst      *xcore.Instance
	SocksPort int
	HTTPPort  int
}

// StartTunnel builds and starts an instance with a local SOCKS inbound on
// 127.0.0.1:socksPort and an HTTP inbound on socksPort+1, both feeding the given
// outbound. When fragment is set the tunnel fragments the TLS ClientHello (for
// servers the tester found need it). Call Close to stop it.
func StartTunnel(outboundJSON []byte, socksPort int, fragment bool) (*Tunnel, error) {
	httpPort := socksPort + 1
	sniffing := map[string]any{"enabled": true, "destOverride": []any{"http", "tls"}}
	extra := map[string]any{
		"inbounds": []any{
			map[string]any{
				"tag": "socks-in", "listen": "127.0.0.1", "port": socksPort,
				"protocol": "socks",
				"settings": map[string]any{"udp": true, "auth": "noauth"},
				"sniffing": sniffing,
			},
			map[string]any{
				"tag": "http-in", "listen": "127.0.0.1", "port": httpPort,
				"protocol": "http",
				"settings": map[string]any{},
				"sniffing": sniffing,
			},
		},
	}
	inst, err := buildInstance(outboundJSON, fragment, extra)
	if err != nil {
		return nil, err
	}
	if err := inst.Start(); err != nil {
		return nil, fmt.Errorf("start tunnel: %w", err)
	}
	return &Tunnel{inst: inst, SocksPort: socksPort, HTTPPort: httpPort}, nil
}

// Close stops the tunnel and releases the local port.
func (t *Tunnel) Close() error {
	if t == nil || t.inst == nil {
		return nil
	}
	return t.inst.Close()
}

// MeasureDelay returns a server's median real-ping latency in milliseconds, or
// an error if the probe did not reach the (filtered) target through it. It is a
// thin wrapper over ProbeDelay for callers that want a plain latency/error
// rather than the classified verdict.
func MeasureDelay(ctx context.Context, outboundJSON []byte, timeout time.Duration, fragment bool) (int64, error) {
	res := ProbeDelay(ctx, outboundJSON, timeout, fragment)
	if res.PingMs < 0 {
		return -1, fmt.Errorf("probe failed: %s (%s)", res.Verdict, res.Detail)
	}
	return res.PingMs, nil
}

// MeasureSpeed downloads a test file through the given outbound and reports the
// download throughput in megabits per second, along with the number of bytes
// transferred. Only the body-download phase is timed (connection/TLS setup is
// excluded). The download is capped by `window`: whatever transfers within it
// is used to compute the rate, so a slow link still yields a result rather than
// hanging.
func MeasureSpeed(ctx context.Context, outboundJSON []byte, window time.Duration, fragment bool) (mbps float64, bytes int64, err error) {
	inst, err := startMeasureInstance(outboundJSON, fragment)
	if err != nil {
		return 0, 0, err
	}
	defer inst.Close()

	client := &http.Client{Transport: proxyTransport(inst)}
	var lastErr error
	for _, url := range speedURLs {
		n, elapsed, err := downloadThrough(ctx, client, url, window)
		if err != nil {
			lastErr = err
			continue
		}
		return float64(n) * 8 / elapsed / 1e6, n, nil
	}
	return 0, 0, lastErr
}

// downloadThrough GETs url with client and times only the body-download phase,
// stopping after window elapses (partial transfer is a valid sample). Returns
// bytes read and seconds elapsed.
func downloadThrough(ctx context.Context, client *http.Client, url string, window time.Duration) (int64, float64, error) {
	// Allow connection/TLS setup time on top of the measurement window.
	reqCtx, cancel := context.WithTimeout(ctx, window+8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("speed endpoint http %d", resp.StatusCode)
	}

	// Stop after the window by closing the body, which unblocks the in-flight read.
	stop := time.AfterFunc(window, func() { resp.Body.Close() })
	defer stop.Stop()

	start := time.Now()
	n, copyErr := io.Copy(io.Discard, resp.Body)
	elapsed := time.Since(start).Seconds()

	if n == 0 {
		if copyErr != nil {
			return 0, 0, copyErr
		}
		return 0, 0, fmt.Errorf("no data received")
	}
	if elapsed <= 0 {
		elapsed = 0.001
	}
	return n, elapsed, nil
}
