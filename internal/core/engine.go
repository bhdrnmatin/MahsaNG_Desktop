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
	"fmt"
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

// measureURL is the latency probe target. A 204 response means "reached the
// open internet through the proxy". Same endpoint v2rayNG uses.
const measureURL = "https://www.google.com/generate_204"

// buildInstance creates (but does not start) an xray instance whose primary
// outbound is the given outbound JSON object. extraJSON is merged at the top
// level of the config (used to add a SOCKS inbound for the live tunnel).
func buildInstance(outboundJSON []byte, extra map[string]any) (*xcore.Instance, error) {
	full := map[string]any{
		"log":       map[string]any{"loglevel": "none"},
		"outbounds": []any{mustObject(outboundJSON)},
	}
	for k, v := range extra {
		full[k] = v
	}
	cfgBytes, err := jsonMarshal(full)
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

// Tunnel is a running xray instance exposing a local SOCKS5 (and HTTP) proxy
// that routes traffic through the chosen server. This is the M1 "CONNECT":
// apps/browser point at 127.0.0.1:<port>. (M2 will add system-wide TUN.)
type Tunnel struct {
	inst    *xcore.Instance
	SocksPort int
}

// StartTunnel builds and starts an instance with a local SOCKS+HTTP inbound on
// 127.0.0.1:socksPort feeding the given outbound. Call Close to stop it.
func StartTunnel(outboundJSON []byte, socksPort int) (*Tunnel, error) {
	extra := map[string]any{
		"inbounds": []any{map[string]any{
			"tag":      "socks-in",
			"listen":   "127.0.0.1",
			"port":     socksPort,
			"protocol": "socks",
			"settings": map[string]any{"udp": true, "auth": "noauth"},
			"sniffing": map[string]any{"enabled": true, "destOverride": []any{"http", "tls"}},
		}},
	}
	inst, err := buildInstance(outboundJSON, extra)
	if err != nil {
		return nil, err
	}
	if err := inst.Start(); err != nil {
		return nil, fmt.Errorf("start tunnel: %w", err)
	}
	return &Tunnel{inst: inst, SocksPort: socksPort}, nil
}

// Close stops the tunnel and releases the local port.
func (t *Tunnel) Close() error {
	if t == nil || t.inst == nil {
		return nil
	}
	return t.inst.Close()
}

// MeasureDelay starts a throwaway instance for the given outbound and times an
// HTTP request to the probe URL routed through it. Returns latency in
// milliseconds, or an error if the server is unreachable within timeout.
func MeasureDelay(ctx context.Context, outboundJSON []byte, timeout time.Duration) (int64, error) {
	inst, err := buildInstance(outboundJSON, nil)
	if err != nil {
		return -1, err
	}
	if err := inst.Start(); err != nil {
		return -1, fmt.Errorf("start: %w", err)
	}
	defer inst.Close()

	tr := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dest, err := xnet.ParseDestination(network + ":" + addr)
			if err != nil {
				return nil, err
			}
			return xcore.Dial(ctx, inst, dest)
		},
	}
	client := &http.Client{Transport: tr, Timeout: timeout}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, measureURL, nil)
	if err != nil {
		return -1, err
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()
	return time.Since(start).Milliseconds(), nil
}
