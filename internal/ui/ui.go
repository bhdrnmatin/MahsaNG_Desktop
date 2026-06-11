// Package ui is the Fyne desktop front-end for the MahsaNG Go port. It renders
// the main page (config list + GET CONFIG / TEST / SORT / filter / connect) and
// wires every action to the already-verified engine in internal/core, provider,
// tester and parser.
//
// Data model: a.all is the single source of truth. a.view holds the indices
// into a.all that are currently shown (after the provider filter), in display
// order (re-ordered by SORT). All mutation of a.all/a.view happens on the Fyne
// main goroutine; background work marshals back via fyne.Do.
package ui

import (
	"context"
	"fmt"
	"image/color"
	"sort"

	fyne "fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"mahsang/internal/core"
	"mahsang/internal/model"
	"mahsang/internal/provider"
	"mahsang/internal/tester"
)

const (
	socksPort  = 10809
	maxServers = 20 // GET CONFIG caps the list to this many, spread across providers
)

// App holds the running UI state.
type App struct {
	fyneApp fyne.App
	win     fyne.Window

	providers []provider.Provider

	all  []model.Config // master list
	view []int          // indices into all, in display order

	list     *widget.List
	status   *widget.Label
	filterBtn *widget.Button
	connBtn  *widget.Button

	filter   string // "" = All, otherwise a provider name
	selected int    // index into all, or -1

	tunnel  *core.Tunnel
	busy    bool // a fetch/test is running
	cancel  context.CancelFunc
}

// New builds the application and its window.
func New() *App {
	a := &App{
		fyneApp:   app.NewWithID("net.mahsang.desktop"),
		providers: provider.Builtins(),
		selected:  -1,
	}
	a.win = a.fyneApp.NewWindow("MahsaNG")
	a.win.Resize(fyne.NewSize(560, 760))
	a.win.SetContent(a.buildContent())
	a.win.SetOnClosed(func() {
		if a.tunnel != nil {
			a.tunnel.Close()
		}
	})
	return a
}

// Run shows the window and blocks until it closes.
func (a *App) Run() { a.win.ShowAndRun() }

// --- layout -----------------------------------------------------------------

func (a *App) buildContent() fyne.CanvasObject {
	title := canvas.NewText("MahsaNG", color.NRGBA{R: 0x2e, G: 0xb8, B: 0x72, A: 0xff})
	title.TextSize = 20
	title.TextStyle = fyne.TextStyle{Bold: true}
	top := container.NewHBox(title, layout.NewSpacer(), widget.NewLabel("desktop"))

	a.list = widget.NewList(a.rowCount, a.rowTemplate, a.rowUpdate)
	a.list.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(a.view) {
			a.selected = a.view[id]
		}
	}
	a.list.OnUnselected = func(widget.ListItemID) { a.selected = -1 }

	getBtn := widget.NewButton("GET CONFIG", a.onGetConfig)
	testBtn := widget.NewButton("TEST", a.onTest)
	sortBtn := widget.NewButton("SORT", a.onSort)
	a.filterBtn = widget.NewButton("All", a.onCycleFilter)
	buttons := container.NewGridWithColumns(4, getBtn, testBtn, sortBtn, a.filterBtn)

	a.connBtn = widget.NewButton("Connect", a.onConnectToggle)
	a.connBtn.Importance = widget.HighImportance
	a.status = widget.NewLabel("Ready. Press GET CONFIG.")
	bottom := container.NewVBox(buttons, a.connBtn, a.status)

	return container.NewBorder(top, bottom, nil, nil, a.list)
}

// --- list rows --------------------------------------------------------------

func (a *App) rowCount() int { return len(a.view) }

func (a *App) rowTemplate() fyne.CanvasObject {
	name := widget.NewLabel("")
	name.Truncation = fyne.TextTruncateEllipsis
	nameWrap := container.NewGridWrap(fyne.NewSize(230, 32), name)

	prov := canvas.NewText("", color.NRGBA{R: 0x3b, G: 0x82, B: 0xf6, A: 0xff}) // blue
	prov.TextSize = 12
	proto := canvas.NewText("", color.NRGBA{R: 0xf5, G: 0x9e, B: 0x0b, A: 0xff}) // orange
	proto.TextSize = 12
	ping := canvas.NewText("", color.Gray{Y: 0x99})
	ping.TextSize = 13
	ping.TextStyle = fyne.TextStyle{Bold: true}

	// Stable child order: [0]=nameWrap [1]=spacer [2]=prov [3]=proto [4]=ping
	return container.NewHBox(nameWrap, layout.NewSpacer(), prov, proto, ping)
}

func (a *App) rowUpdate(id widget.ListItemID, obj fyne.CanvasObject) {
	if id < 0 || id >= len(a.view) {
		return
	}
	c := a.all[a.view[id]]
	row := obj.(*fyne.Container)
	nameWrap := row.Objects[0].(*fyne.Container)
	nameWrap.Objects[0].(*widget.Label).SetText(c.Name)

	prov := row.Objects[2].(*canvas.Text)
	prov.Text = c.Provider
	prov.Refresh()

	proto := row.Objects[3].(*canvas.Text)
	proto.Text = c.Protocol
	proto.Refresh()

	ping := row.Objects[4].(*canvas.Text)
	ping.Text, ping.Color = pingLabel(c.PingMs)
	ping.Refresh()
}

func pingLabel(ms int64) (string, color.Color) {
	switch {
	case ms < 0:
		return "—", color.Gray{Y: 0x88}
	case ms < 800:
		return fmt.Sprintf("%dms", ms), color.NRGBA{R: 0x22, G: 0xc5, B: 0x5e, A: 0xff} // green
	case ms < 2000:
		return fmt.Sprintf("%dms", ms), color.NRGBA{R: 0xf5, G: 0x9e, B: 0x0b, A: 0xff} // orange
	default:
		return fmt.Sprintf("%dms", ms), color.NRGBA{R: 0xef, G: 0x44, B: 0x44, A: 0xff} // red
	}
}

// --- actions ----------------------------------------------------------------

func (a *App) onGetConfig() {
	if a.busy {
		return
	}
	a.setBusy(true)
	a.setStatus("Fetching configs…")
	go func() {
		ctx := context.Background()
		fetched := provider.Collect(ctx, a.providers, maxServers)
		fyne.Do(func() {
			a.all = fetched
			a.filter = ""
			a.filterBtn.SetText("All")
			a.rebuildView()
			a.list.UnselectAll()
			a.selected = -1
			a.setBusy(false)
			if len(a.all) == 0 {
				a.setStatus("No configs returned. Check provider/network.")
			} else {
				a.setStatus(fmt.Sprintf("Got %d configs. Press TEST.", len(a.all)))
			}
		})
	}()
}

func (a *App) onTest() {
	if a.busy || len(a.view) == 0 {
		return
	}
	a.setBusy(true)
	idxs := append([]int(nil), a.view...) // snapshot of visible indices
	testConfigs := make([]model.Config, len(idxs))
	for i, gi := range idxs {
		testConfigs[i] = a.all[gi]
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	total := len(testConfigs)
	done := 0

	go func() {
		tester.TestAll(ctx, testConfigs, func(r tester.Result) {
			gi := idxs[r.Index]
			fyne.Do(func() {
				a.all[gi].PingMs = r.PingMs
				done++
				if done%8 == 0 || done == total {
					a.list.Refresh()
					a.setStatus(fmt.Sprintf("Testing… %d/%d", done, total))
				}
			})
		})
		fyne.Do(func() {
			a.cancel = nil
			a.setBusy(false)
			a.setStatus(fmt.Sprintf("Tested %d configs. Press SORT.", total))
		})
	}()
}

func (a *App) onSort() {
	if a.busy {
		return
	}
	sort.SliceStable(a.view, func(i, j int) bool {
		ai, bi := a.all[a.view[i]].PingMs, a.all[a.view[j]].PingMs
		if ai < 0 {
			return false
		}
		if bi < 0 {
			return true
		}
		return ai < bi
	})
	a.list.UnselectAll()
	a.selected = -1
	a.list.Refresh()
	a.setStatus("Sorted by ping (fastest first).")
}

func (a *App) onCycleFilter() {
	if a.busy {
		return
	}
	options := append([]string{""}, a.distinctProviders()...)
	// find current and advance
	cur := 0
	for i, o := range options {
		if o == a.filter {
			cur = i
			break
		}
	}
	a.filter = options[(cur+1)%len(options)]
	if a.filter == "" {
		a.filterBtn.SetText("All")
	} else {
		a.filterBtn.SetText(a.filter)
	}
	a.list.UnselectAll()
	a.selected = -1
	a.rebuildView()
}

func (a *App) onConnectToggle() {
	if a.tunnel != nil {
		a.tunnel.Close()
		a.tunnel = nil
		a.connBtn.SetText("Connect")
		a.setStatus("Disconnected.")
		return
	}
	if a.selected < 0 {
		a.setStatus("Select a config first, then Connect.")
		return
	}
	c := a.all[a.selected]
	a.setStatus("Connecting…")
	go func() {
		t, err := core.StartTunnel(c.Outbound, socksPort)
		fyne.Do(func() {
			if err != nil {
				a.setStatus("Connect failed: " + err.Error())
				return
			}
			a.tunnel = t
			a.connBtn.SetText("Disconnect")
			a.setStatus(fmt.Sprintf("Connected via %s — SOCKS5 127.0.0.1:%d", c.Name, socksPort))
		})
	}()
}

// --- helpers ----------------------------------------------------------------

func (a *App) rebuildView() {
	a.view = a.view[:0]
	for i := range a.all {
		if a.filter == "" || a.all[i].Provider == a.filter {
			a.view = append(a.view, i)
		}
	}
	a.list.Refresh()
}

func (a *App) distinctProviders() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, c := range a.all {
		if _, ok := seen[c.Provider]; !ok && c.Provider != "" {
			seen[c.Provider] = struct{}{}
			out = append(out, c.Provider)
		}
	}
	sort.Strings(out)
	return out
}

func (a *App) setStatus(s string) { a.status.SetText(s) }

func (a *App) setBusy(b bool) { a.busy = b }
