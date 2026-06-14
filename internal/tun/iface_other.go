//go:build !linux

package tun

// DefaultInterface returns "" off Linux: interface-bound serverless TUN
// (SO_BINDTODEVICE) is Linux-only for now, so serverless falls back to
// local-proxy mode on other platforms.
func DefaultInterface() string { return "" }
