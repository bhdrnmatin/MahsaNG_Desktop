package ui

import (
	"image/color"
	"testing"

	fyne "fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	"mahsang/internal/model"
)

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
