//go:build !linux && !windows && !darwin

package tun

import "errors"

const tunName = "mahsa-tun"

// TUN routing is implemented for Linux, Windows and macOS; Start returns this
// on any other platform.
var errUnsupported = errors.New("TUN mode is not implemented on this platform")

func configureRoutes(dev string, serverIPs []string) error { return errUnsupported }
func restoreRoutes(dev string) error                       { return nil }
