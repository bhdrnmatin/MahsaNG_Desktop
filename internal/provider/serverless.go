package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"mahsang/internal/model"
)

// Serverless is a provider for feeds that ship whole xray configs (their own
// fragment/noise/routing, no proxy server) as a JSON array, rather than share
// links. patterniha/Serverless-for-Iran is the reference source. Each array
// element becomes one model.Config carrying the raw config.
type Serverless struct {
	ProviderName string
	URL          string
	HTTP         *http.Client
}

func NewServerless(name, url string) *Serverless {
	return &Serverless{
		ProviderName: name,
		URL:          url,
		HTTP:         &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *Serverless) Name() string { return s.ProviderName }

func (s *Serverless) Fetch(ctx context.Context) ([]model.Config, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "v2rayNG/1.8.0")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: fetch: %w", s.ProviderName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: http %d", s.ProviderName, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	// Keep each config as raw JSON so its exact form reaches xray-core untouched;
	// only peek at "remarks" for the display name.
	var entries []json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("%s: parse config array: %w", s.ProviderName, err)
	}
	out := make([]model.Config, 0, len(entries))
	for _, raw := range entries {
		var meta struct {
			Remarks string `json:"remarks"`
		}
		_ = json.Unmarshal(raw, &meta)
		name := meta.Remarks
		if name == "" {
			name = s.ProviderName
		}
		out = append(out, model.Config{
			// Synthetic stable id so the rest of the app (dedup, failed-tracking,
			// connected marker, persistence) can key on Link as it does for links.
			Link:      "serverless://" + name,
			Name:      name,
			Protocol:  "SERVERLESS",
			Provider:  s.ProviderName,
			RawConfig: []byte(raw),
			// Serverless configs are never probed (no server to throttle; the
			// strict probe gives false failures from outside Iran) — shown as a
			// fixed "available" fallback. See [[serverless-provider-work]].
			PingMs: model.PingAvailable,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no configs found", s.ProviderName)
	}
	return out, nil
}
