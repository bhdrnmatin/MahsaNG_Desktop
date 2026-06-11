//go:build !linux

package tun

import "errors"

// TUN routing is currently implemented for Linux only. Windows (wintun + netsh
// routing) is planned; until then Start returns this on other platforms.
var errUnsupported = errors.New("TUN mode is only implemented on Linux so far")

func configureRoutes(dev string, serverIPs []string) error { return errUnsupported }
func restoreRoutes(dev string) error                       { return nil }
