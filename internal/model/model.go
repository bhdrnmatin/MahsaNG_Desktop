// Package model holds the shared data types passed between the provider,
// parser, tester and UI layers.
package model

// Sentinel values for Config.PingMs (a real measurement is >= 0).
const (
	PingUntested int64 = -2 // not tested yet
	PingFailed   int64 = -1 // tested, but the server returned no result
)

// Verdict classifies *why* a probe through a tunnel succeeded or failed, mapping
// the observed behaviour to the censorship mechanism most likely behind it. The
// point is that "failed" is not one thing: a RST mid-handshake, a silent
// timeout, and a 200 carrying the block page each implicate a different filter
// and call for a different evasion, so we keep them apart instead of collapsing
// everything to PingFailed.
type Verdict int8

const (
	VerdictUntested   Verdict = iota // not probed yet
	VerdictOK                        // real egress: reached the filtered target, body genuine
	VerdictReset                     // RST/refused -> DPI fingerprinted the protocol or SNI
	VerdictTimeout                   // no reply, no RST -> IP black-hole or throttle
	VerdictBlockPage                 // HTTP reply, but the censor's page (DNS poison / intercept)
	VerdictProxyError                // xray instance wouldn't build/start: bad config, not the network
	VerdictThrottled                 // handshake+headers OK, then the flow was cut mid-body (byte-limit/throttle)
)

func (v Verdict) String() string {
	switch v {
	case VerdictOK:
		return "ok"
	case VerdictReset:
		return "reset"
	case VerdictTimeout:
		return "timeout"
	case VerdictBlockPage:
		return "blockpage"
	case VerdictProxyError:
		return "proxyerror"
	case VerdictThrottled:
		return "throttled"
	default:
		return "untested"
	}
}

// ProbeResult is the classified outcome of one delay probe. PingMs is valid
// (>= 0) only when Verdict is VerdictOK; for every other verdict it is
// PingFailed. Detail carries the raw error/status for logging.
type ProbeResult struct {
	Verdict Verdict
	PingMs  int64
	Detail  string
}

// Config is one VPN server entry, parsed from a share link.
type Config struct {
	// Link is the original share link (vmess://, vless://, trojan://, ss://).
	Link string
	// Name is the human label shown in the list (the "remarks" of the link).
	Name string
	// Protocol is uppercase: VMESS, VLESS, TROJAN, SHADOWSOCKS.
	Protocol string
	// Provider is the source that supplied this config (e.g. "Mahsa", "YeBeKhe").
	Provider string
	// Outbound is the xray-core outbound JSON for this server, used to build a
	// measuring instance and (later) the live tunnel. Filled by the parser.
	Outbound []byte
	// PingMs is the last measured real-ping latency in milliseconds, or one of
	// the PingUntested / PingFailed sentinels.
	PingMs int64
	// LastVerdict is why the last probe passed or failed (which censorship
	// mechanism it points to). VerdictUntested until the first probe.
	LastVerdict Verdict
	// Fragment means this server only works with TLS-ClientHello fragmentation
	// (SNI-based DPI resets it otherwise). Discovered by the tester's auto-retry
	// and then used for every probe and the live tunnel.
	Fragment bool
}
