package store

import (
	"testing"

	"mahsang/internal/model"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := []model.Config{
		{Link: "trojan://pw@host.example:443#srv", Provider: "P1", PingMs: 120},
		{Link: "vless://uuid@example.com:443?security=tls#v", Provider: "Clipboard", PingMs: model.PingFailed},
		{Link: "not-a-link", Provider: "P2", PingMs: 5}, // must be dropped on load
	}
	data, err := encode(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 configs (unparseable dropped), got %d", len(out))
	}
	if out[0].Link != in[0].Link || out[0].Provider != "P1" || out[0].PingMs != 120 {
		t.Fatalf("first config mangled: %+v", out[0])
	}
	if out[0].Name == "" || out[0].Protocol != "TROJAN" || len(out[0].Outbound) == 0 {
		t.Fatalf("first config not re-parsed: %+v", out[0])
	}
	if out[1].PingMs != model.PingFailed {
		t.Fatalf("ping not preserved: %+v", out[1])
	}
}

func TestDecodeMalformed(t *testing.T) {
	if _, err := decode([]byte("{not json")); err == nil {
		t.Fatal("expected error for malformed file")
	}
}

// TestSaveLoadFile exercises the real file path: first Load on a fresh config
// dir returns empty (no error), then Save + Load round-trips.
func TestSaveLoadFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir) // Linux/macOS
	t.Setenv("AppData", dir)         // Windows
	t.Setenv("SUDO_USER", "")        // never divert to a sudo home in tests

	if got, err := Load(); err != nil || len(got) != 0 {
		t.Fatalf("fresh Load = %d configs, err %v; want empty, nil", len(got), err)
	}
	in := []model.Config{{Link: "trojan://pw@host.example:443#srv", Provider: "P1", PingMs: 88}}
	if err := Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load()
	if err != nil || len(out) != 1 {
		t.Fatalf("Load after Save = %d configs, err %v; want 1, nil", len(out), err)
	}
	if out[0].Link != in[0].Link || out[0].PingMs != 88 || out[0].Protocol != "TROJAN" {
		t.Fatalf("round-trip mangled: %+v", out[0])
	}
}
