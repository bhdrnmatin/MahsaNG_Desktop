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

// Collect fetches every provider, de-duplicates by link, and returns at most
// limit configs interleaved round-robin across providers (so the result is a
// spread of sources, not all from whichever provider returned first). Each
// provider's list is shuffled first, so repeated calls return a random
// selection rather than always the same head of the list. limit <= 0 means no
// cap. Providers that error are skipped.
func Collect(ctx context.Context, providers []Provider, limit int) []model.Config {
	lists := make([][]model.Config, len(providers))
	for i, p := range providers {
		if cs, err := p.Fetch(ctx); err == nil {
			shuffled := make([]model.Config, len(cs))
			copy(shuffled, cs)
			rand.Shuffle(len(shuffled), func(a, b int) {
				shuffled[a], shuffled[b] = shuffled[b], shuffled[a]
			})
			lists[i] = shuffled
		}
	}
	seen := make(map[string]struct{})
	var out []model.Config
	for col := 0; ; col++ {
		progressed := false
		for _, l := range lists {
			if col >= len(l) {
				continue
			}
			progressed = true
			c := l[col]
			if _, dup := seen[c.Link]; dup {
				continue
			}
			seen[c.Link] = struct{}{}
			out = append(out, c)
			if limit > 0 && len(out) >= limit {
				return out
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
	HTTP         *http.Client
}

func NewSubscription(name, url string) *Subscription {
	return &Subscription{
		ProviderName: name,
		URL:          url,
		HTTP:         &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *Subscription) Name() string { return s.ProviderName }

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
