//go:build windows

package tun

import "golang.org/x/sys/windows"

// Elevated reports whether the process has the privileges TUN setup needs
// (an elevated / Administrator token on Windows).
func Elevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}
