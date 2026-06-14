package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"mahsang/internal/store"
)

// Serverless configs route on geosite:/geoip: rules, which xray-core can only
// evaluate with geosite.dat / geoip.dat on disk (located via XRAY_LOCATION_ASSET).
// Normal proxy outbounds never need these, so rather than bundle ~30 MB into the
// binary we fetch them on first use and cache them in the app data dir.
var geoAssets = []struct{ name, url string }{
	{"geoip.dat", "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat"},
	{"geosite.dat", "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat"},
}

var (
	geoOnce sync.Once
	geoErr  error
)

// EnsureGeoAssets makes sure geoip.dat/geosite.dat exist in <appdir>/geo and
// points xray-core at them via XRAY_LOCATION_ASSET. The download happens at most
// once per process; later calls return the first result. It blocks until the
// assets are ready, so a serverless instance built right after is guaranteed to
// find them.
func EnsureGeoAssets() error {
	geoOnce.Do(func() { geoErr = downloadGeoAssets() })
	return geoErr
}

func downloadGeoAssets() error {
	base, err := store.Dir()
	if err != nil {
		return fmt.Errorf("geo assets: %w", err)
	}
	dir := filepath.Join(base, "geo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("geo assets: %w", err)
	}
	// Point xray at the dir regardless; some assets may already be present.
	os.Setenv("XRAY_LOCATION_ASSET", dir)

	client := &http.Client{Timeout: 5 * time.Minute}
	for _, a := range geoAssets {
		dst := filepath.Join(dir, a.name)
		if fi, err := os.Stat(dst); err == nil && fi.Size() > 0 {
			continue // already cached
		}
		if err := downloadFile(client, a.url, dst); err != nil {
			return fmt.Errorf("geo assets: download %s: %w", a.name, err)
		}
	}
	return nil
}

func downloadFile(client *http.Client, url, dst string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
