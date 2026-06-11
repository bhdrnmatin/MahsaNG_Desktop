//go:build !windows

package tun

import "os"

// Elevated reports whether the process has the privileges TUN setup needs
// (root on unix-like systems).
func Elevated() bool {
	return os.Geteuid() == 0
}
