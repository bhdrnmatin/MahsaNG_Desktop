package provider

import (
	"context"
	"fmt"
	"testing"

	"mahsang/internal/model"
)

// stub is a Provider returning a fixed list, for offline testing. weight 0 is
// treated as the default (1) by Collect.
type stub struct {
	name   string
	weight int
	cfgs   []model.Config
}

func (s stub) Name() string                                  { return s.name }
func (s stub) Weight() int                                   { return s.weight }
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
	a := stub{name: "A", cfgs: makeConfigs("A", "a1", "a2", "a3", "a4", "a5")}
	b := stub{name: "B", cfgs: makeConfigs("B", "b1", "b2", "b3", "b4", "b5")}

	got := Collect(context.Background(), []Provider{a, b}, 4)
	if len(got) != 4 {
		t.Fatalf("limit not honored: got %d, want 4", len(got))
	}
	// Even spread across providers holds regardless of shuffle (each provider
	// has 5 configs; limit 4 -> round-robin takes 2 from each).
	counts := map[string]int{}
	for _, c := range got {
		counts[c.Provider]++
	}
	if counts["A"] != 2 || counts["B"] != 2 {
		t.Fatalf("expected even spread 2/2, got %v", counts)
	}
}

func TestCollectRandomizes(t *testing.T) {
	// With a list far larger than the cap, two calls should (almost surely)
	// return different selections.
	links := make([]string, 200)
	for i := range links {
		links[i] = fmt.Sprintf("c%d", i)
	}
	p := stub{name: "A", cfgs: makeConfigs("A", links...)}
	first := linkSet(Collect(context.Background(), []Provider{p}, 10))
	for try := 0; try < 5; try++ {
		if !sameSet(first, linkSet(Collect(context.Background(), []Provider{p}, 10))) {
			return // differs -> randomized, as expected
		}
	}
	t.Fatal("Collect returned the same 10 configs across 6 calls; not randomized")
}

func linkSet(cs []model.Config) map[string]struct{} {
	m := make(map[string]struct{}, len(cs))
	for _, c := range cs {
		m[c.Link] = struct{}{}
	}
	return m
}

func sameSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func TestCollectDedups(t *testing.T) {
	// Overlapping links across providers must appear once.
	a := stub{name: "A", cfgs: makeConfigs("A", "x", "y")}
	b := stub{name: "B", cfgs: makeConfigs("B", "x", "z")} // "x" duplicates A

	got := Collect(context.Background(), []Provider{a, b}, 0) // no cap
	if len(got) != 3 {
		t.Fatalf("dedup failed: got %d (%v), want 3 unique", len(got), links(got))
	}
}

func TestCollectWeighted(t *testing.T) {
	// A has weight 3, B weight 1. Each round A contributes 3 and B 1, so with a
	// cap of 8 (two full rounds) expect A:6, B:2.
	aLinks := make([]string, 20)
	bLinks := make([]string, 20)
	for i := range aLinks {
		aLinks[i] = fmt.Sprintf("a%d", i)
		bLinks[i] = fmt.Sprintf("b%d", i)
	}
	a := stub{name: "A", weight: 3, cfgs: makeConfigs("A", aLinks...)}
	b := stub{name: "B", weight: 1, cfgs: makeConfigs("B", bLinks...)}

	counts := map[string]int{}
	for _, c := range Collect(context.Background(), []Provider{a, b}, 8) {
		counts[c.Provider]++
	}
	if counts["A"] != 6 || counts["B"] != 2 {
		t.Fatalf("expected weighted 6/2 (A/B), got %v", counts)
	}
}

func links(cs []model.Config) string {
	s := ""
	for _, c := range cs {
		s += fmt.Sprintf("%s ", c.Link)
	}
	return s
}
