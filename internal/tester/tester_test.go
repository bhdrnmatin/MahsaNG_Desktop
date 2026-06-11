package tester

import (
	"context"
	"fmt"
	"net"
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
