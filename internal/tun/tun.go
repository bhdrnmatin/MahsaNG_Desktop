// Package tun provides system-wide routing: it creates a TUN interface and
// uses tun2socks (gVisor netstack) to forward all OS traffic into the app's
// local SOCKS proxy. Unlike the system-proxy approach, this captures traffic
// from every app, including ones that ignore proxy settings.
//
// Requires root/admin (creating a TUN device and editing the routing table).
// The real server's IP is excluded from the tunnel so its own traffic does not
// loop back through it.
package tun

import (
	"fmt"
	"sync"

	"github.com/xjasonlyu/tun2socks/v2/engine"
)

const (
	tunName = "mahsa-tun"
	tunCIDR = "198.18.0.1/15" // benchmarking range; unlikely to collide
	tunMTU  = 1500
)

var (
	mu        sync.Mutex
	running   bool
	savedGW   string   // original default gateway, for restore
	savedDev  string   // original default interface
	hostRoute []string // per-server host routes we added, for cleanup
)

// Start brings up the TUN, points tun2socks at socksAddr (host:port), excludes
// serverIPs from the tunnel, and routes all other traffic through it.
func Start(socksAddr string, serverIPs []string) error {
	mu.Lock()
	defer mu.Unlock()
	if running {
		return nil
	}
	engine.Insert(&engine.Key{
		Device:   "tun://" + tunName,
		Proxy:    "socks5://" + socksAddr,
		MTU:      tunMTU,
		LogLevel: "silent",
	})
	engine.Start()

	if err := configureRoutes(tunName, serverIPs); err != nil {
		engine.Stop()
		return fmt.Errorf("configure routes: %w", err)
	}
	running = true
	return nil
}

// Stop restores the routing table and tears down the TUN.
func Stop() error {
	mu.Lock()
	defer mu.Unlock()
	if !running {
		return nil
	}
	err := restoreRoutes(tunName)
	engine.Stop()
	running = false
	return err
}

// Running reports whether the TUN is currently active.
func Running() bool {
	mu.Lock()
	defer mu.Unlock()
	return running
}
