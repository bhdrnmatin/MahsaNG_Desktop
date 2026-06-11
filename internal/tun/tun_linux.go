//go:build linux

package tun

import (
	"fmt"
	"os/exec"
	"strings"
)

// configureRoutes assigns the TUN an address, excludes each server IP via the
// original gateway, then overrides the default route with the TUN using the
// 0/1 + 128/1 split (which beats the existing default without deleting it).
func configureRoutes(dev string, serverIPs []string) error {
	gw, odev, err := defaultRoute()
	if err != nil {
		return err
	}
	savedGW, savedDev = gw, odev
	hostRoute = nil

	if err := run("ip", "addr", "add", tunCIDR, "dev", dev); err != nil {
		return err
	}
	if err := run("ip", "link", "set", dev, "up"); err != nil {
		return err
	}
	// Keep the server's own traffic off the tunnel.
	for _, ip := range serverIPs {
		if ip == "" {
			continue
		}
		if err := run("ip", "route", "add", ip+"/32", "via", gw, "dev", odev); err == nil {
			hostRoute = append(hostRoute, ip)
		}
	}
	if err := run("ip", "route", "add", "0.0.0.0/1", "dev", dev); err != nil {
		return err
	}
	if err := run("ip", "route", "add", "128.0.0.0/1", "dev", dev); err != nil {
		return err
	}
	return nil
}

func restoreRoutes(dev string) error {
	// Best-effort teardown in reverse; ignore individual errors so we always
	// try to remove everything.
	_ = run("ip", "route", "del", "0.0.0.0/1", "dev", dev)
	_ = run("ip", "route", "del", "128.0.0.0/1", "dev", dev)
	for _, ip := range hostRoute {
		_ = run("ip", "route", "del", ip+"/32", "via", savedGW, "dev", savedDev)
	}
	hostRoute = nil
	_ = run("ip", "link", "set", dev, "down")
	return nil
}

// defaultRoute parses `ip route show default` -> gateway and interface.
func defaultRoute() (gw, dev string, err error) {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return "", "", err
	}
	// e.g. "default via 192.168.1.1 dev eth0 proto dhcp metric 100"
	fields := strings.Fields(string(out))
	for i := 0; i+1 < len(fields); i++ {
		switch fields[i] {
		case "via":
			gw = fields[i+1]
		case "dev":
			dev = fields[i+1]
		}
	}
	if gw == "" || dev == "" {
		return "", "", fmt.Errorf("could not parse default route: %q", string(out))
	}
	return gw, dev, nil
}

func run(name string, args ...string) error {
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
