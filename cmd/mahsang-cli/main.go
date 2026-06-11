// Command mahsang-cli is the M0 verification spine: it exercises the engine
// end-to-end (fetch -> parse -> test -> sort) without the GUI, so the
// xray-core integration can be proven before any UI is built.
//
// Usage:
//
//	mahsang-cli get                 # fetch from built-in providers, list configs
//	mahsang-cli test <subURL|->     # fetch a subscription URL, ping all, sorted
//	mahsang-cli ping <share-link>   # measure one link
//	mahsang-cli connect <link>      # start a local SOCKS tunnel, verify egress IP
//	mahsang-cli speed <link>        # download throughput through one link
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"mahsang/internal/core"
	"mahsang/internal/model"
	"mahsang/internal/parser"
	"mahsang/internal/provider"
	"mahsang/internal/tester"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: mahsang-cli <get|test|ping|connect|speed> [arg]")
		os.Exit(2)
	}
	ctx := context.Background()
	switch os.Args[1] {
	case "get":
		cmdGet(ctx)
	case "test":
		cmdTest(ctx, arg(2), arg(3))
	case "ping":
		cmdPing(ctx, arg(2))
	case "connect":
		cmdConnect(ctx, arg(2))
	case "speed":
		cmdSpeed(ctx, arg(2))
	default:
		fmt.Println("unknown command:", os.Args[1])
		os.Exit(2)
	}
}

func arg(i int) string {
	if len(os.Args) > i {
		return os.Args[i]
	}
	return ""
}

func cmdGet(ctx context.Context) {
	configs := fetchBuiltins(ctx)
	fmt.Printf("\nFetched %d configs:\n", len(configs))
	for i, c := range configs {
		fmt.Printf("  %3d  [%-11s] %-8s  %s\n", i+1, c.Protocol, c.Provider, c.Name)
	}
}

func cmdTest(ctx context.Context, src, limit string) {
	var configs []model.Config
	if src == "" {
		configs = fetchBuiltins(ctx)
	} else {
		var err error
		configs, err = provider.NewSubscription("custom", src).Fetch(ctx)
		if err != nil {
			fail(err)
		}
	}
	if n, _ := strconv.Atoi(limit); n > 0 && n < len(configs) {
		configs = configs[:n]
	}
	fmt.Printf("Testing %d configs...\n", len(configs))
	done := 0
	tester.TestAll(ctx, configs, func(r tester.Result) {
		done++
		fmt.Printf("\r  tested %d/%d", done, len(configs))
	})
	fmt.Println()
	tester.SortByPing(configs)
	fmt.Println("\nResults (fastest first):")
	for _, c := range configs {
		ping := "fail"
		if c.PingMs >= 0 {
			ping = fmt.Sprintf("%dms", c.PingMs)
		}
		fmt.Printf("  %-7s [%-11s] %-8s %s\n", ping, c.Protocol, c.Provider, c.Name)
	}
}

func cmdPing(ctx context.Context, link string) {
	if link == "" {
		// read from stdin
		sc := bufio.NewScanner(os.Stdin)
		if sc.Scan() {
			link = strings.TrimSpace(sc.Text())
		}
	}
	c, err := parser.Parse(link)
	if err != nil {
		fail(err)
	}
	fmt.Printf("Parsed: [%s] %s\nOutbound: %s\n\n", c.Protocol, c.Name, c.Outbound)
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ping, err := core.MeasureDelay(cctx, c.Outbound, 12*time.Second)
	if err != nil {
		fmt.Printf("ping: FAILED (%v)\n", err)
		return
	}
	fmt.Printf("ping: %dms\n", ping)
}

func cmdSpeed(ctx context.Context, link string) {
	c, err := parser.Parse(link)
	if err != nil {
		fail(err)
	}
	fmt.Printf("Speed testing [%s] %s …\n", c.Protocol, c.Name)
	mbps, n, err := core.MeasureSpeed(ctx, c.Outbound, 10*time.Second)
	if err != nil {
		fmt.Printf("speed: FAILED (%v)\n", err)
		return
	}
	fmt.Printf("speed: %.1f Mbps (%.1f MB downloaded)\n", mbps, float64(n)/(1<<20))
}

func cmdConnect(ctx context.Context, link string) {
	c, err := parser.Parse(link)
	if err != nil {
		fail(err)
	}
	const port = 10809
	tun, err := core.StartTunnel(c.Outbound, port)
	if err != nil {
		fail(err)
	}
	defer tun.Close()
	fmt.Printf("Connected via [%s] %s\n", c.Protocol, c.Name)
	fmt.Printf("SOCKS5 proxy: 127.0.0.1:%d\n", port)

	// Verify real traffic flows through the proxy by fetching our egress IP.
	dialer, _ := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", port), nil, proxy.Direct)
	httpc := &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{Dial: dialer.Dial},
	}
	resp, err := httpc.Get("https://api.ipify.org")
	if err != nil {
		fmt.Printf("proxy check FAILED: %v\n", err)
		return
	}
	defer resp.Body.Close()
	ip, _ := io.ReadAll(resp.Body)
	fmt.Printf("egress IP through tunnel: %s\n", strings.TrimSpace(string(ip)))
}

func fetchBuiltins(ctx context.Context) []model.Config {
	var all []model.Config
	for _, p := range provider.Builtins() {
		fmt.Printf("Fetching from %s...\n", p.Name())
		cs, err := p.Fetch(ctx)
		if err != nil {
			fmt.Printf("  ! %v\n", err)
			continue
		}
		fmt.Printf("  + %d configs\n", len(cs))
		all = append(all, cs...)
	}
	return all
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
