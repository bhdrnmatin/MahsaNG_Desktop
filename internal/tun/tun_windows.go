//go:build windows

package tun

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const tunName = "mahsa-tun"

// hostRoute holds the per-server exclusion routes we added, for cleanup
// (guarded by the package mutex in tun.go).
var hostRoute []string

// configureRoutes assigns the wintun adapter an address, excludes each server
// IP via the original gateway, then overrides the default route with the TUN
// using the same 0/1 + 128/1 split as the Linux implementation (it beats the
// existing default without deleting it).
func configureRoutes(dev string, serverIPs []string) error {
	gw, err := defaultGateway()
	if err != nil {
		log.Printf("[tun-win] defaultGateway failed: %v", err)
		return err
	}
	hostRoute = nil

	tunIP, tunMask, err := cidrToAddrMask(tunCIDR)
	if err != nil {
		return err
	}
	log.Printf("[tun-win] default gateway=%s, tun adapter=%s addr=%s mask=%s", gw, dev, tunIP, tunMask)
	// The wintun adapter can take a moment to register after the engine
	// starts, so retry the address assignment briefly.
	err = retry(10, 300*time.Millisecond, func() error {
		return run("netsh", "interface", "ipv4", "set", "address",
			"name="+dev, "source=static", "address="+tunIP, "mask="+tunMask)
	})
	if err != nil {
		return err
	}
	// Keep the server's own traffic off the tunnel.
	for _, ip := range serverIPs {
		if ip == "" || strings.Contains(ip, ":") {
			continue // IPv4 routes only, matching the Linux implementation
		}
		if err := run("route", "add", ip, "mask", "255.255.255.255", gw, "metric", "5"); err == nil {
			hostRoute = append(hostRoute, ip)
		}
	}
	// Override the default route with the 0/1 + 128/1 split, pinned to the TUN
	// interface explicitly. A bare `route add 0.0.0.0 mask 128.0.0.0 <tunIP>`
	// lets Windows pick the interface for the next hop and it mis-binds the
	// route to the physical adapter (where the TUN's own IP isn't reachable), so
	// the route is treated as dead and real traffic keeps going out the LAN.
	// netsh add route with interface=<dev> forces the route onto the TUN.
	for _, prefix := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if err := run("netsh", "interface", "ipv4", "add", "route",
			"prefix="+prefix, "interface="+dev, "nexthop="+tunIP,
			"metric=1", "store=active"); err != nil {
			return err
		}
	}
	return nil
}

func restoreRoutes(dev string) error {
	// Best-effort teardown; ignore individual errors so we always try to
	// remove everything. The wintun adapter itself disappears with the engine.
	if tunIP, _, err := cidrToAddrMask(tunCIDR); err == nil {
		for _, prefix := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
			_ = run("netsh", "interface", "ipv4", "delete", "route",
				"prefix="+prefix, "interface="+dev, "nexthop="+tunIP)
		}
	}
	for _, ip := range hostRoute {
		_ = run("route", "delete", ip)
	}
	hostRoute = nil
	return nil
}

// defaultGateway parses `route print -4 0.0.0.0` for the active IPv4 default
// gateway. Expected row: "0.0.0.0  0.0.0.0  <gateway>  <interface>  <metric>".
func defaultGateway() (string, error) {
	out, err := command("route", "print", "-4", "0.0.0.0").Output()
	if err != nil {
		return "", fmt.Errorf("route print: %w", err)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 5 && f[0] == "0.0.0.0" && f[1] == "0.0.0.0" && net.ParseIP(f[2]) != nil {
			return f[2], nil
		}
	}
	return "", fmt.Errorf("could not find IPv4 default gateway in route table")
}

// cidrToAddrMask converts "198.18.0.1/15" into the address / dotted-mask pair
// netsh and route expect ("198.18.0.1", "255.254.0.0").
func cidrToAddrMask(cidr string) (string, string, error) {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", err
	}
	m := ipnet.Mask
	if len(m) != 4 {
		return "", "", fmt.Errorf("not an IPv4 CIDR: %s", cidr)
	}
	return ip.String(), fmt.Sprintf("%d.%d.%d.%d", m[0], m[1], m[2], m[3]), nil
}

func retry(attempts int, delay time.Duration, f func() error) error {
	var err error
	for range attempts {
		if err = f(); err == nil {
			return nil
		}
		time.Sleep(delay)
	}
	return err
}

// command builds an exec.Cmd that won't flash a console window when the app
// runs as a GUI process.
func command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func run(name string, args ...string) error {
	out, err := command(name, args...).CombinedOutput()
	if err != nil {
		log.Printf("[tun-win] FAIL: %s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	log.Printf("[tun-win] ok: %s %s", name, strings.Join(args, " "))
	return nil
}
