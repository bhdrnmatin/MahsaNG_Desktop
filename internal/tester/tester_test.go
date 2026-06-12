package tester

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"

	"mahsang/internal/model"
)

// cfgFor builds a minimal config whose outbound points at addr ("host:port").
func cfgFor(t *testing.T, name, addr string) model.Config {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("bad addr %q: %v", addr, err)
	}
	ob := fmt.Sprintf(`{"protocol":"trojan","settings":{"servers":[{"address":%q,"port":%s}]}}`, host, port)
	return model.Config{Name: name, Link: "trojan://x@" + addr, Outbound: []byte(ob)}
}

func TestFilterAlive(t *testing.T) {
	// Two real listeners = alive; one immediately-closed port = dead
	// (localhost refusal is instant, so no slow timeout in the test).
	l1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l1.Close()
	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := dead.Addr().String()
	dead.Close()

	in := []model.Config{
		cfgFor(t, "alive1", l1.Addr().String()),
		cfgFor(t, "dead", deadAddr),
		{Name: "unparseable", Link: "x", Outbound: []byte(`{}`)},
		cfgFor(t, "alive2", l2.Addr().String()),
	}
	out := FilterAlive(context.Background(), in)
	if len(out) != 2 || out[0].Name != "alive1" || out[1].Name != "alive2" {
		t.Fatalf("expected [alive1 alive2] in order, got %d: %+v", len(out), names(out))
	}
}

func TestFilterAliveCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	// Cancelled context: must return promptly and mark nothing alive.
	out := FilterAlive(ctx, []model.Config{cfgFor(t, "a", l.Addr().String())})
	if len(out) != 0 {
		t.Fatalf("expected no results under cancelled ctx, got %d", len(out))
	}
}

func names(cs []model.Config) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name
	}
	return out
}

// TestTestAllRetriesPreviouslyGood stubs the prober so every call fails, and
// checks the retry policy: a config with a previous real ping is probed
// twice, while untested / previously-failed ones are probed once.
func TestTestAllRetriesPreviouslyGood(t *testing.T) {
	orig := probe
	defer func() { probe = orig }()

	var mu sync.Mutex
	calls := map[string]int{}
	probe = func(_ context.Context, outbound []byte, _ bool) model.ProbeResult {
		mu.Lock()
		calls[string(outbound)]++
		mu.Unlock()
		return model.ProbeResult{Verdict: model.VerdictTimeout, PingMs: model.PingFailed}
	}

	configs := []model.Config{
		{Name: "was-good", Outbound: []byte("good"), PingMs: 300},
		{Name: "untested", Outbound: []byte("new"), PingMs: model.PingUntested},
		{Name: "was-dead", Outbound: []byte("dead"), PingMs: model.PingFailed},
	}
	TestAll(context.Background(), configs, nil)

	if calls["good"] != 2 {
		t.Fatalf("previously-good config probed %d times, want 2", calls["good"])
	}
	if calls["new"] != 1 || calls["dead"] != 1 {
		t.Fatalf("untested/failed configs probed %d/%d times, want 1/1", calls["new"], calls["dead"])
	}
	for _, c := range configs {
		if c.PingMs != model.PingFailed {
			t.Fatalf("%s: PingMs = %d, want PingFailed", c.Name, c.PingMs)
		}
		if c.LastVerdict != model.VerdictTimeout {
			t.Fatalf("%s: LastVerdict = %v, want timeout", c.Name, c.LastVerdict)
		}
	}
}

// TestTestAllRetrySucceeds: the second sample's result must win.
func TestTestAllRetrySucceeds(t *testing.T) {
	orig := probe
	defer func() { probe = orig }()

	var mu sync.Mutex
	n := 0
	probe = func(context.Context, []byte, bool) model.ProbeResult {
		mu.Lock()
		defer mu.Unlock()
		n++
		if n == 1 {
			return model.ProbeResult{Verdict: model.VerdictTimeout, PingMs: model.PingFailed} // transient failure
		}
		return model.ProbeResult{Verdict: model.VerdictOK, PingMs: 280}
	}

	configs := []model.Config{{Name: "flaky", Outbound: []byte("x"), PingMs: 300}}
	TestAll(context.Background(), configs, nil)

	if configs[0].PingMs != 280 {
		t.Fatalf("PingMs = %d, want 280 (retry result)", configs[0].PingMs)
	}
	if configs[0].LastVerdict != model.VerdictOK {
		t.Fatalf("LastVerdict = %v, want ok (retry result)", configs[0].LastVerdict)
	}
}

// TestTestAllAutoFragment: a server that resets without fragmentation but works
// with it must be retried fragmented, marked Fragment, and report the success.
func TestTestAllAutoFragment(t *testing.T) {
	orig := probe
	defer func() { probe = orig }()

	probe = func(_ context.Context, _ []byte, fragment bool) model.ProbeResult {
		if fragment {
			return model.ProbeResult{Verdict: model.VerdictOK, PingMs: 150}
		}
		return model.ProbeResult{Verdict: model.VerdictReset, PingMs: model.PingFailed}
	}

	configs := []model.Config{{Name: "needs-frag", Outbound: []byte("x"), PingMs: model.PingUntested}}
	TestAll(context.Background(), configs, nil)

	c := configs[0]
	if !c.Fragment {
		t.Fatal("Fragment = false, want true (auto-retry should have flagged it)")
	}
	if c.PingMs != 150 || c.LastVerdict != model.VerdictOK {
		t.Fatalf("got PingMs=%d verdict=%v, want 150/ok (fragmented result)", c.PingMs, c.LastVerdict)
	}
}
