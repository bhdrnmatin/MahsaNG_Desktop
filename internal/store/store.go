// Package store persists the config list between runs as a small JSON file in
// the user's config directory, so the app starts with the previously fetched
// and tested servers instead of an empty list.
package store

import (
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"mahsang/internal/model"
	"mahsang/internal/parser"
)

// entry is the persisted form of one config: just enough to rebuild it with
// parser.Parse. Name/Protocol/Outbound are re-derived from the link on load,
// so the file stays small and survives changes to the outbound JSON format.
type entry struct {
	Link     string        `json:"link"`
	Provider string        `json:"provider"`
	PingMs   int64         `json:"ping_ms"`
	Verdict  model.Verdict `json:"verdict"`
}

// Load reads the saved list and re-parses each link. A missing file is not an
// error: it returns an empty list (first run).
func Load() ([]model.Config, error) {
	path, err := filePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decode(data)
}

// Save writes the list atomically (temp file + rename), creating the config
// directory if needed.
func Save(configs []model.Config) error {
	path, err := filePath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := encode(configs)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	fixOwnership(dir, path)
	return nil
}

func encode(configs []model.Config) ([]byte, error) {
	entries := make([]entry, len(configs))
	for i, c := range configs {
		entries[i] = entry{Link: c.Link, Provider: c.Provider, PingMs: c.PingMs, Verdict: c.LastVerdict}
	}
	return json.MarshalIndent(entries, "", "  ")
}

func decode(data []byte) ([]model.Config, error) {
	var entries []entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	out := make([]model.Config, 0, len(entries))
	for _, e := range entries {
		c, err := parser.Parse(e.Link)
		if err != nil {
			continue // link no longer parseable; drop it
		}
		c.Provider = e.Provider
		c.PingMs = e.PingMs
		c.LastVerdict = e.Verdict
		out = append(out, c)
	}
	return out, nil
}

// filePath returns <config-dir>/mahsang/configs.json. The app is normally
// launched with sudo (Connect needs root), so when running elevated we use
// the invoking user's home instead of root's — both modes then see the same
// list.
func filePath() (string, error) {
	if su := os.Getenv("SUDO_USER"); su != "" && os.Geteuid() == 0 {
		if u, err := user.Lookup(su); err == nil {
			return filepath.Join(u.HomeDir, ".config", "mahsang", "configs.json"), nil
		}
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mahsang", "configs.json"), nil
}

// fixOwnership hands files created by a sudo run back to the invoking user,
// so later unprivileged runs can read and update them. No-op when not root
// (and on Windows, where Geteuid is always -1).
func fixOwnership(paths ...string) {
	uid, errU := strconv.Atoi(os.Getenv("SUDO_UID"))
	gid, errG := strconv.Atoi(os.Getenv("SUDO_GID"))
	if os.Geteuid() != 0 || errU != nil || errG != nil {
		return
	}
	for _, p := range paths {
		_ = os.Chown(p, uid, gid)
	}
}
