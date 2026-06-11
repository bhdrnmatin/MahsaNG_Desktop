//go:build darwin

package tun

import (
	"fmt"
	"os/exec"
	"strings"
)

// macOS only lets a TUN device claim a utunN name; a fixed high unit keeps it
// predictable for the route commands below (and unlikely to be taken).
const tunName = "utun123"

// Routing state saved by configureRoutes for restoreRoutes (guarded by the
// package mutex in tun.go).
var (
	savedGW   string   // original default gateway, for restore
	hostRoute []string // per-server host routes we added, for cleanup
)

// configureRoutes assigns the utun its point-to-point address, excludes each
// server IP via the original gateway, then overrides the default route with
// the 0/1 + 128/1 split (which beats the existing default without deleting
// it), mirroring the Linux implementation.
func configureRoutes(dev string, serverIPs []string) error {
	gw, err := defaultGateway()
	if err != nil {
		return err
	}
	savedGW = gw
	hostRoute = nil

	tunIP, _, _ := strings.Cut(tunCIDR, "/")
	// utun is point-to-point: local and peer address are both the TUN IP.
	if err := run("ifconfig", dev, tunIP, tunIP, "up"); err != nil {
		return err
	}
	// Keep the server's own traffic off the tunnel.
	for _, ip := range serverIPs {
		if ip == "" || strings.Contains(ip, ":") {
			continue // IPv4 routes only, matching the Linux implementation
		}
		if err := run("route", "-q", "-n", "add", "-host", ip, gw); err == nil {
			hostRoute = append(hostRoute, ip)
		}
	}
	if err := run("route", "-q", "-n", "add", "-net", "0.0.0.0/1", "-interface", dev); err != nil {
		return err
	}
	if err := run("route", "-q", "-n", "add", "-net", "128.0.0.0/1", "-interface", dev); err != nil {
		return err
	}
	return nil
}

func restoreRoutes(dev string) error {
	// Best-effort teardown; ignore individual errors so we always try to
	// remove everything. The utun itself disappears with the engine.
	_ = run("route", "-q", "-n", "delete", "-net", "0.0.0.0/1", "-interface", dev)
	_ = run("route", "-q", "-n", "delete", "-net", "128.0.0.0/1", "-interface", dev)
	for _, ip := range hostRoute {
		_ = run("route", "-q", "-n", "delete", "-host", ip, savedGW)
	}
	hostRoute = nil
	return nil
}

// defaultGateway parses `route -n get default` for the "gateway:" line.
func defaultGateway() (string, error) {
	out, err := exec.Command("route", "-n", "get", "default").Output()
	if err != nil {
		return "", fmt.Errorf("route get default: %w", err)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), ":"); ok && strings.TrimSpace(k) == "gateway" {
			return strings.TrimSpace(v), nil
		}
	}
	return "", fmt.Errorf("could not find default gateway: %q", string(out))
}

func run(name string, args ...string) error {
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
