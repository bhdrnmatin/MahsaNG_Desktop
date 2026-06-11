package provider

import (
	"context"
	"fmt"
	"testing"

	"mahsang/internal/model"
)

// stub is a Provider returning a fixed list, for offline testing.
type stub struct {
	name string
	cfgs []model.Config
}

func (s stub) Name() string { return s.name }
func (s stub) Fetch(context.Context) ([]model.Config, error) { return s.cfgs, nil }

func makeConfigs(provName string, links ...string) []model.Config {
	out := make([]model.Config, len(links))
	for i, l := range links {
		out[i] = model.Config{Link: l, Provider: provName, PingMs: -1}
	}
	return out
}

func TestCollectCapsAndSpreads(t *testing.T) {
	// Two providers with 5 configs each; cap at 4 -> expect 2 from each (round-robin).
	a := stub{"A", makeConfigs("A", "a1", "a2", "a3", "a4", "a5")}
	b := stub{"B", makeConfigs("B", "b1", "b2", "b3", "b4", "b5")}

	got := Collect(context.Background(), []Provider{a, b}, 4)
	if len(got) != 4 {
		t.Fatalf("limit not honored: got %d, want 4", len(got))
	}
	counts := map[string]int{}
	for _, c := range got {
		counts[c.Provider]++
	}
	if counts["A"] != 2 || counts["B"] != 2 {
		t.Fatalf("expected even spread 2/2, got %v", counts)
	}
	// Round-robin order: a1, b1, a2, b2
	wantOrder := []string{"a1", "b1", "a2", "b2"}
	for i, w := range wantOrder {
		if got[i].Link != w {
			t.Fatalf("order mismatch at %d: got %q want %q (full: %v)", i, got[i].Link, w, links(got))
		}
	}
}

func TestCollectDedups(t *testing.T) {
	// Overlapping links across providers must appear once.
	a := stub{"A", makeConfigs("A", "x", "y")}
	b := stub{"B", makeConfigs("B", "x", "z")} // "x" duplicates A

	got := Collect(context.Background(), []Provider{a, b}, 0) // no cap
	if len(got) != 3 {
		t.Fatalf("dedup failed: got %d (%v), want 3 unique", len(got), links(got))
	}
}

func links(cs []model.Config) string {
	s := ""
	for _, c := range cs {
		s += fmt.Sprintf("%s ", c.Link)
	}
	return s
}
