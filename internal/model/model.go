// Package model holds the shared data types passed between the provider,
// parser, tester and UI layers.
package model

// Sentinel values for Config.PingMs (a real measurement is >= 0).
const (
	PingUntested int64 = -2 // not tested yet
	PingFailed   int64 = -1 // tested, but the server returned no result
)

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
}
