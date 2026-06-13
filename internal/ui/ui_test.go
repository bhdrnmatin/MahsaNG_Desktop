package ui

import (
	"image/color"
	"testing"
	"time"

	fyne "fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	"mahsang/internal/model"
)

// TestFailedSkipExpiry checks the remember-and-skip logic: a fresh failure is
// "recent" (GET CONFIG skips it), one older than failedTTL is not, and
// pruneFailed drops the expired entry.
func TestFailedSkipExpiry(t *testing.T) {
	a := &App{failed: map[string]time.Time{
		"fresh": time.Now(),
		"old":   time.Now().Add(-2 * failedTTL),
	}}
	if !a.isFailedRecently("fresh") {
		t.Error("fresh failure should count as recent")
	}
	if a.isFailedRecently("old") {
		t.Error("failure older than failedTTL should have expired")
	}
	if a.isFailedRecently("unknown") {
		t.Error("never-failed link should not be recent")
	}
	a.pruneFailed()
	if _, ok := a.failed["old"]; ok {
		t.Error("pruneFailed should drop the expired entry")
	}
	if _, ok := a.failed["fresh"]; !ok {
		t.Error("pruneFailed should keep the fresh entry")
	}
}

// TestRowUpdateConnectedMarker verifies the row template/update indexing is
// correct (no panic) and that the left bar is green only for the connected
// config. Runs headlessly: it never calls New()/app.New(), so no window or GL
// context is created.
func TestRowUpdateConnectedMarker(t *testing.T) {
	a := &App{connected: -1}
	a.all = []model.Config{
		{Name: "alpha", Provider: "P1", Protocol: "VLESS", Link: "l0", PingMs: 123},
		{Name: "beta", Provider: "P2", Protocol: "VMESS", Link: "l1", PingMs: -1},
	}
	a.view = []int{0, 1}

	row := a.rowTemplate().(*fyne.Container)
	bar := row.Objects[1].(*canvas.Rectangle)
	content := row.Objects[0].(*fyne.Container)
	name := content.Objects[0].(*fyne.Container).Objects[0].(*widget.Label)

	// Row 0, nothing connected -> transparent bar, name populated.
	a.rowUpdate(0, row)
	if name.Text != "alpha" {
		t.Fatalf("name not set: got %q", name.Text)
	}
	if bar.FillColor != color.Transparent {
		t.Fatalf("row 0 bar should be transparent, got %v", bar.FillColor)
	}

	// Connect config at global index 1, render that row -> green bar.
	a.connected = 1
	a.rowUpdate(1, row)
	if name.Text != "beta" {
		t.Fatalf("name not updated: got %q", name.Text)
	}
	green, ok := bar.FillColor.(color.NRGBA)
	if !ok || green.G < 0x80 || green.A == 0 {
		t.Fatalf("connected row bar should be opaque green, got %v", bar.FillColor)
	}

	// Render a non-connected row again -> back to transparent.
	a.rowUpdate(0, row)
	if bar.FillColor != color.Transparent {
		t.Fatalf("non-connected row bar should be transparent, got %v", bar.FillColor)
	}
}

// TestOnDeleteInvalid reproduces the crash where the list refreshed against a
// stale view after a.all shrank: it must remove failed configs without panic.
func TestOnDeleteInvalid(t *testing.T) {
	a := &App{connected: -1, selected: -1}
	a.all = []model.Config{
		{Name: "good", Link: "l0", Provider: "P", Protocol: "VLESS", PingMs: 120},
		{Name: "dead", Link: "l1", Provider: "P", Protocol: "VMESS", PingMs: model.PingFailed},
		{Name: "fresh", Link: "l2", Provider: "P", Protocol: "TROJAN", PingMs: model.PingUntested},
	}
	a.view = []int{0, 1, 2}
	a.status = widget.NewLabel("")
	a.list = widget.NewList(a.rowCount, a.rowTemplate, a.rowUpdate)

	a.onDeleteInvalid() // must not panic

	if len(a.all) != 2 {
		t.Fatalf("expected 2 configs after removing 1 failed, got %d", len(a.all))
	}
	for _, c := range a.all {
		if c.PingMs == model.PingFailed {
			t.Fatalf("a failed config survived: %q", c.Name)
		}
	}
	if len(a.view) != 2 {
		t.Fatalf("view out of sync with all: view=%d all=%d", len(a.view), len(a.all))
	}
}

func TestTransportClass(t *testing.T) {
	cases := []struct {
		outbound string
		want     string
	}{
		{`{"streamSettings":{"network":"ws","security":"tls"}}`, "CDN"},
		{`{"streamSettings":{"network":"grpc"}}`, "CDN"},
		{`{"streamSettings":{"network":"tcp","security":"tls"}}`, "direct"},
		{`{"streamSettings":{"network":"tcp","security":"reality"}}`, "direct"},
		{`{"streamSettings":{"network":"tcp"}}`, "direct"},
		{`not json`, "direct"},
	}
	for _, c := range cases {
		if got := transportClass([]byte(c.outbound)); got != c.want {
			t.Errorf("transportClass(%s) = %q, want %q", c.outbound, got, c.want)
		}
	}
}

func TestIndexOfLink(t *testing.T) {
	cfgs := []model.Config{{Link: "a"}, {Link: "b"}}
	if got := indexOfLink(cfgs, "b"); got != 1 {
		t.Fatalf("indexOfLink(b) = %d, want 1", got)
	}
	if got := indexOfLink(cfgs, "missing"); got != -1 {
		t.Fatalf("indexOfLink(missing) = %d, want -1", got)
	}
	if got := indexOfLink(cfgs, ""); got != -1 {
		t.Fatalf("indexOfLink(\"\") = %d, want -1", got)
	}
}
