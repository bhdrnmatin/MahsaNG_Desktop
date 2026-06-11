// Package provider fetches VPN configs from sources and turns them into parsed
// model.Config entries. Each source implements Provider, so the Mahsa feed and
// any third-party subscription are interchangeable from the UI's point of view.
package provider

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"time"

	"mahsang/internal/model"
	"mahsang/internal/parser"
)

// Provider is one config source ("GET CONFIG" pulls from all enabled providers).
type Provider interface {
	Name() string
	Fetch(ctx context.Context) ([]model.Config, error)
}

// Weighted is an optional interface: a provider's weight is how many configs it
// contributes per round-robin round in Collect, so higher-weight (better)
// providers make up a proportionally larger share of the capped selection.
// Providers that don't implement it are treated as weight 1.
type Weighted interface {
	Weight() int
}

func weightOf(p Provider) int {
	if w, ok := p.(Weighted); ok && w.Weight() > 0 {
		return w.Weight()
	}
	return 1
}

// Collect fetches every provider, de-duplicates by link, and returns at most
// limit configs interleaved round-robin across providers (so the result is a
// spread of sources, not all from whichever provider returned first). Each
// round, a provider contributes up to its Weight configs, so higher-weight
// (better) providers make up a larger share of the result. Each provider's list
// is shuffled first, so repeated calls return a random selection rather than
// always the same head of the list. limit <= 0 means no cap. Providers that
// error are skipped.
func Collect(ctx context.Context, providers []Provider, limit int) []model.Config {
	type source struct {
		list   []model.Config
		pos    int
		weight int
	}
	sources := make([]source, len(providers))
	for i, p := range providers {
		s := source{weight: weightOf(p)}
		if cs, err := p.Fetch(ctx); err == nil {
			s.list = make([]model.Config, len(cs))
			copy(s.list, cs)
			rand.Shuffle(len(s.list), func(a, b int) {
				s.list[a], s.list[b] = s.list[b], s.list[a]
			})
		}
		sources[i] = s
	}
	seen := make(map[string]struct{})
	var out []model.Config
	for {
		progressed := false
		for i := range sources {
			s := &sources[i]
			for k := 0; k < s.weight && s.pos < len(s.list); k++ {
				progressed = true
				c := s.list[s.pos]
				s.pos++
				if _, dup := seen[c.Link]; dup {
					continue
				}
				seen[c.Link] = struct{}{}
				out = append(out, c)
				if limit > 0 && len(out) >= limit {
					return out
				}
			}
		}
		if !progressed {
			break
		}
	}
	return out
}

// Subscription is a generic provider that downloads a URL and extracts every
// share link it can find — handling plaintext lists, base64-wrapped lists, and
// links embedded in JSON. This covers the overwhelming majority of v2ray/xray
// subscription endpoints, including the public sources MahsaNG itself uses.
type Subscription struct {
	ProviderName string
	URL          string
	WeightVal    int // round-robin weight; <= 0 means 1
	HTTP         *http.Client
}

func NewSubscription(name, url string) *Subscription {
	return NewWeightedSubscription(name, url, 1)
}

func NewWeightedSubscription(name, url string, weight int) *Subscription {
	return &Subscription{
		ProviderName: name,
		URL:          url,
		WeightVal:    weight,
		HTTP:         &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *Subscription) Name() string { return s.ProviderName }

func (s *Subscription) Weight() int { return s.WeightVal }

func (s *Subscription) Fetch(ctx context.Context) ([]model.Config, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, err
	}
	// Some subscription hosts gate on a v2ray-ish user agent.
	req.Header.Set("User-Agent", "v2rayNG/1.8.0")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: fetch: %w", s.ProviderName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: http %d", s.ProviderName, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	links := ExtractLinks(body)
	if len(links) == 0 {
		return nil, fmt.Errorf("%s: no configs found", s.ProviderName)
	}

	out := make([]model.Config, 0, len(links))
	for _, l := range links {
		c, err := parser.Parse(l)
		if err != nil {
			continue // skip unsupported/malformed entries, keep the rest
		}
		c.Provider = s.ProviderName
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: found %d links but none parsed", s.ProviderName, len(links))
	}
	return out, nil
}

var linkRe = regexp.MustCompile(`(?:vmess|vless|trojan|ss)://[^\s"'<>\\]+`)

// ExtractLinks pulls share links out of a subscription body. If the body is a
// base64 blob (the common case), it is decoded first; links are then matched
// both in the decoded text and the raw text so JSON-embedded links are caught.
func ExtractLinks(body []byte) []string {
	text := string(body)
	candidates := text
	if decoded, ok := tryBase64Block(text); ok {
		candidates = decoded + "\n" + text
	}
	seen := map[string]struct{}{}
	var out []string
	for _, m := range linkRe.FindAllString(candidates, -1) {
		m = strings.TrimRight(m, ",")
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}
