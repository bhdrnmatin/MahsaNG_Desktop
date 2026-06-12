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
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	fyne "fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"mahsang/internal/core"
	"mahsang/internal/model"
	"mahsang/internal/parser"
	"mahsang/internal/provider"
	"mahsang/internal/store"
	"mahsang/internal/tester"
	"mahsang/internal/tun"
)

const (
	socksPort  = 10809
	maxServers = 100 // GET CONFIG caps the list to this many, spread across providers
	// testTargetGood is how many working servers TEST ALL looks for before it
	// stops probing the rest, so finding a few live servers does not wait out the
	// whole dead tail's timeouts.
	testTargetGood = 10
)

// App holds the running UI state.
type App struct {
	fyneApp fyne.App
	win     fyne.Window

	providers []provider.Provider

	all  []model.Config // master list
	view []int          // indices into all, in display order

	list    *widget.List
	status  *widget.Label
	connBtn *widget.Button

	filter    string // "" = all providers (reserved; no UI filter currently)
	selected  int    // index into all, or -1
	connected int    // index into all of the connected config, or -1

	tunnel *core.Tunnel
	busy   bool // a fetch/test is running
	cancel context.CancelFunc
}

// New builds the application and its window.
func New() *App {
	a := &App{
		fyneApp:   app.NewWithID("net.mahsang.desktop"),
		providers: provider.Builtins(),
		selected:  -1,
		connected: -1,
	}
	a.win = a.fyneApp.NewWindow("MahsaNG")
	a.win.Resize(fyne.NewSize(560, 760))
	a.win.SetContent(a.buildContent())
	a.win.SetOnClosed(func() {
		if a.cancel != nil {
			a.cancel() // stop an in-flight TEST ALL
		}
		a.persist() // catch-all; mutations also save as they happen
		// Don't leave the system routed through a dead tunnel on exit.
		_ = tun.Stop()
		if a.tunnel != nil {
			a.tunnel.Close()
		}
	})

	// Restore the previous session's list (with ping results) so the app is
	// usable immediately instead of starting empty.
	if saved, err := store.Load(); err == nil && len(saved) > 0 {
		a.all = saved
		a.rebuildView()
		a.setStatus(fmt.Sprintf("Loaded %d saved configs.", len(saved)))
	}
	return a
}

// Run shows the window and blocks until it closes.
func (a *App) Run() { a.win.ShowAndRun() }

// --- layout -----------------------------------------------------------------

func (a *App) buildContent() fyne.CanvasObject {
	title := canvas.NewText("MahsaNG", color.NRGBA{R: 0x2e, G: 0xb8, B: 0x72, A: 0xff})
	title.TextSize = 20
	title.TextStyle = fyne.TextStyle{Bold: true}
	addBtn := widget.NewButtonWithIcon("", theme.ContentAddIcon(), a.onAddClipboard)

	credit := canvas.NewText("powered by MATOK", color.Gray{Y: 0x99})
	credit.TextSize = 12
	ghURL, _ := url.Parse("https://github.com/matinbhdrn77")
	tgURL, _ := url.Parse("https://t.me/mat_bh")
	creditRow := container.NewHBox(credit,
		widget.NewHyperlink("github.com/matinbhdrn77", ghURL),
		widget.NewHyperlink("telegram:@mat_bh", tgURL))

	top := container.NewVBox(
		container.NewHBox(title, layout.NewSpacer(), addBtn),
		creditRow,
	)

	a.list = widget.NewList(a.rowCount, a.rowTemplate, a.rowUpdate)
	a.list.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(a.view) {
			a.selected = a.view[id]
		}
	}
	a.list.OnUnselected = func(widget.ListItemID) { a.selected = -1 }

	getBtn := widget.NewButton("GET CONFIG", a.onGetConfig)
	testBtn := widget.NewButton("TEST ALL", a.onTest)
	sortBtn := widget.NewButton("SORT", a.onSort)
	delBtn := widget.NewButton("Delete Invalid", a.onDeleteInvalid)
	buttons := container.NewGridWithColumns(4, getBtn, testBtn, sortBtn, delBtn)

	a.connBtn = widget.NewButton("Connect", a.onConnectToggle)
	a.connBtn.Importance = widget.HighImportance
	speedBtn := widget.NewButton("Speed Test", a.onSpeedTest)
	actionRow := container.NewGridWithColumns(2, a.connBtn, speedBtn)
	a.status = widget.NewLabel("Ready. Press GET CONFIG.")
	bottom := container.NewVBox(buttons, actionRow, a.status)

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

	// Content child order: [0]=nameWrap [1]=spacer [2]=prov [3]=proto [4]=ping
	content := container.NewHBox(nameWrap, layout.NewSpacer(), prov, proto, ping)

	// A vertical bar on the left, green for the connected config (the Border
	// left slot stretches it to full row height). NewBorder(nil,nil,left,nil,
	// content) => Objects = [content, bar].
	bar := canvas.NewRectangle(color.Transparent)
	bar.SetMinSize(fyne.NewSize(5, 1))
	return container.NewBorder(nil, nil, bar, nil, content)
}

func (a *App) rowUpdate(id widget.ListItemID, obj fyne.CanvasObject) {
	if id < 0 || id >= len(a.view) {
		return
	}
	gi := a.view[id]
	if gi < 0 || gi >= len(a.all) {
		return // view briefly out of sync with all; next Refresh fixes it
	}
	c := a.all[gi]

	row := obj.(*fyne.Container)
	content := row.Objects[0].(*fyne.Container)
	bar := row.Objects[1].(*canvas.Rectangle)

	nameWrap := content.Objects[0].(*fyne.Container)
	nameWrap.Objects[0].(*widget.Label).SetText(c.Name)

	prov := content.Objects[2].(*canvas.Text)
	prov.Text = c.Provider
	prov.Refresh()

	proto := content.Objects[3].(*canvas.Text)
	proto.Text = c.Protocol
	if c.Fragment {
		proto.Text += " ✂" // server needs TLS-ClientHello fragmentation
	}
	proto.Refresh()

	ping := content.Objects[4].(*canvas.Text)
	ping.Text, ping.Color = pingLabel(c.PingMs, c.LastVerdict)
	ping.Refresh()

	if gi == a.connected {
		bar.FillColor = color.NRGBA{R: 0x22, G: 0xc5, B: 0x5e, A: 0xff} // green
	} else {
		bar.FillColor = color.Transparent
	}
	bar.Refresh()
}

func pingLabel(ms int64, v model.Verdict) (string, color.Color) {
	switch {
	case ms == model.PingUntested:
		return "—", color.Gray{Y: 0x88}
	case ms < 0: // PingFailed: show *why* it failed, not a useless -1ms
		return verdictLabel(v)
	case ms < 800:
		return fmt.Sprintf("%dms", ms), color.NRGBA{R: 0x22, G: 0xc5, B: 0x5e, A: 0xff} // green
	case ms < 2000:
		return fmt.Sprintf("%dms", ms), color.NRGBA{R: 0xf5, G: 0x9e, B: 0x0b, A: 0xff} // orange
	default:
		return fmt.Sprintf("%dms", ms), color.NRGBA{R: 0xef, G: 0x44, B: 0x44, A: 0xff} // red
	}
}

// verdictLabel renders a failed probe's verdict: a short word naming the
// censorship mechanism, coloured to set network filtering (red/orange/purple)
// apart from a local config fault (gray).
func verdictLabel(v model.Verdict) (string, color.Color) {
	red := color.NRGBA{R: 0xef, G: 0x44, B: 0x44, A: 0xff}
	switch v {
	case model.VerdictReset:
		return "reset", red
	case model.VerdictTimeout:
		return "timeout", color.NRGBA{R: 0xf5, G: 0x9e, B: 0x0b, A: 0xff} // orange
	case model.VerdictBlockPage:
		return "blocked", color.NRGBA{R: 0xa1, G: 0x55, B: 0xf5, A: 0xff} // purple
	case model.VerdictProxyError:
		return "bad cfg", color.Gray{Y: 0x99}
	default:
		return "fail", red
	}
}

// --- actions ----------------------------------------------------------------

func (a *App) onGetConfig() {
	if a.busy {
		return
	}
	a.setBusy(true)
	a.setStatus("Fetching configs…")
	// Remember the connected server's link so its green marker survives the
	// list refresh if it happens to be fetched again (the tunnel keeps running
	// regardless).
	connectedLink := ""
	if a.connected >= 0 {
		connectedLink = a.all[a.connected].Link
	}
	go func() {
		ctx := context.Background()
		// Over-fetch, then keep only TCP-reachable servers: most free configs
		// are dead, and the cheap dial filter means the maxServers slots that
		// get the expensive TEST ALL probe are all plausible candidates.
		fetched := provider.Collect(ctx, a.providers, maxServers*6)
		fyne.Do(func() {
			a.setStatus(fmt.Sprintf("Checking %d configs for reachability…", len(fetched)))
		})
		fetched = tester.FilterAlive(ctx, fetched)
		fyne.Do(func() {
			// Drop tested-and-failed configs, keep working + untested ones,
			// then top up with newly fetched configs (deduped) up to the cap.
			kept := make([]model.Config, 0, len(a.all))
			seen := make(map[string]struct{})
			for _, c := range a.all {
				if c.PingMs == model.PingFailed {
					continue
				}
				kept = append(kept, c)
				seen[c.Link] = struct{}{}
			}
			added := 0
			for _, c := range fetched {
				if len(kept) >= maxServers {
					break
				}
				if _, dup := seen[c.Link]; dup {
					continue
				}
				kept = append(kept, c)
				seen[c.Link] = struct{}{}
				added++
			}
			a.all = kept
			a.rebuildView()
			a.list.UnselectAll()
			a.selected = -1
			a.connected = indexOfLink(a.all, connectedLink)
			a.setBusy(false)
			a.persist()
			if len(a.all) == 0 {
				a.setStatus("No configs returned. Check provider/network.")
			} else {
				a.setStatus(fmt.Sprintf("Added %d new, %d total. Press TEST ALL.", added, len(a.all)))
			}
		})
	}()
}

// onAddClipboard reads the clipboard and adds any config links it finds —
// a single share link, several newline-separated links, or a base64
// subscription blob (handled by provider.ExtractLinks). Duplicates of configs
// already in the list are skipped.
func (a *App) onAddClipboard() {
	if a.busy {
		return // a running TEST ALL holds index snapshots into a.all
	}
	text := a.fyneApp.Clipboard().Content()
	if strings.TrimSpace(text) == "" {
		a.setStatus("Clipboard is empty.")
		return
	}
	links := provider.ExtractLinks([]byte(text))
	if len(links) == 0 {
		a.setStatus("No config links found in clipboard.")
		return
	}
	existing := make(map[string]struct{}, len(a.all))
	for _, c := range a.all {
		existing[c.Link] = struct{}{}
	}
	added := 0
	for _, l := range links {
		if _, dup := existing[l]; dup {
			continue
		}
		c, err := parser.Parse(l)
		if err != nil {
			continue
		}
		c.Provider = "Clipboard"
		a.all = append(a.all, c)
		existing[l] = struct{}{}
		added++
	}
	a.rebuildView()
	if added == 0 {
		a.setStatus("No new configs added (duplicates or unsupported).")
	} else {
		a.persist()
		a.setStatus(fmt.Sprintf("Added %d config(s) from clipboard.", added))
	}
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
	good := 0

	go func() {
		tester.TestAll(ctx, testConfigs, testTargetGood, func(r tester.Result) {
			gi := idxs[r.Index]
			fyne.Do(func() {
				a.all[gi].PingMs = r.PingMs
				a.all[gi].LastVerdict = r.Verdict
				a.all[gi].Fragment = r.Fragment
				done++
				if r.Verdict == model.VerdictOK {
					good++
				}
				if done%8 == 0 {
					a.list.Refresh()
					a.setStatus(fmt.Sprintf("Testing… %d working, %d probed", good, done))
				}
			})
		})
		fyne.Do(func() {
			cancel() // release the context now that all workers are done
			a.cancel = nil
			a.setBusy(false)
			a.persist() // ping results are worth keeping across runs
			a.list.Refresh()
			a.setStatus(fmt.Sprintf("Found %d working (probed %d/%d). Press SORT.", good, done, total))
		})
	}()
}

// onDeleteInvalid removes configs that were tested and returned no result
// (PingMs == PingFailed). Untested configs are kept. The connected server's
// green marker is preserved by re-mapping its index.
func (a *App) onDeleteInvalid() {
	if a.busy {
		return
	}
	connectedLink := ""
	if a.connected >= 0 {
		connectedLink = a.all[a.connected].Link
	}
	kept := make([]model.Config, 0, len(a.all))
	removed := 0
	for _, c := range a.all {
		if c.PingMs == model.PingFailed {
			removed++
			continue
		}
		kept = append(kept, c)
	}
	a.all = kept
	a.connected = indexOfLink(a.all, connectedLink)
	a.selected = -1
	a.rebuildView() // resync a.view to the new a.all BEFORE any refresh
	a.list.UnselectAll()
	if removed == 0 {
		a.setStatus("No invalid configs to remove (run TEST ALL first).")
	} else {
		a.persist()
		a.setStatus(fmt.Sprintf("Removed %d invalid config(s).", removed))
	}
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

// onSpeedTest measures download throughput through the selected config (or the
// connected one if nothing is selected) — a single-server check, not the whole
// list. Result is shown in the status line.
func (a *App) onSpeedTest() {
	if a.busy {
		return
	}
	target := a.selected
	if target < 0 {
		target = a.connected
	}
	if target < 0 {
		a.setStatus("Select a config first, then Speed Test.")
		return
	}
	c := a.all[target]
	a.setBusy(true)
	a.setStatus("Speed testing " + c.Name + " …")
	go func() {
		mbps, n, err := core.MeasureSpeed(context.Background(), c.Outbound, 10*time.Second, c.Fragment)
		fyne.Do(func() {
			a.setBusy(false)
			if err != nil {
				a.setStatus("Speed test failed: " + err.Error())
				return
			}
			a.setStatus(fmt.Sprintf("Speed: %.1f Mbps (%.1f MB downloaded) — %s",
				mbps, float64(n)/(1<<20), c.Name))
		})
	}()
}

// onConnectToggle connects/disconnects. Connecting starts the tunnel and then
// brings up system-wide TUN routing, so ALL OS traffic goes through the selected
// server. This needs root (creating a TUN device + editing the route table).
func (a *App) onConnectToggle() {
	if a.tunnel != nil {
		_ = tun.Stop() // restore routing before the proxy dies
		a.tunnel.Close()
		a.tunnel = nil
		a.connected = -1
		a.connBtn.SetText("Connect")
		a.list.Refresh()
		a.setStatus("Disconnected.")
		return
	}
	if a.busy {
		return // a connect attempt or test is already in flight
	}
	if a.selected < 0 {
		a.setStatus("Select a config first, then Connect.")
		return
	}
	if !tun.Elevated() {
		a.setStatus("System-wide connect needs admin rights — run as root/Administrator.")
		return
	}
	target := a.selected
	c := a.all[target]
	a.setBusy(true)
	a.setStatus("Connecting…")
	go func() {
		t, err := core.StartTunnel(c.Outbound, socksPort, c.Fragment)
		if err != nil {
			fyne.Do(func() {
				a.setBusy(false)
				a.setStatus("Connect failed: " + err.Error())
			})
			return
		}
		// Resolve the server's IP(s) (before TUN takes over DNS) to exclude
		// them from the tunnel and avoid a routing loop.
		var ips []string
		if host, e := core.OutboundServer(c.Outbound); e == nil {
			if addrs, e := net.LookupHost(host); e == nil {
				ips = addrs
			}
		}
		if err := tun.Start(fmt.Sprintf("127.0.0.1:%d", t.SocksPort), ips); err != nil {
			t.Close() // roll back; connect is all-or-nothing (whole system)
			fyne.Do(func() {
				a.setBusy(false)
				a.setStatus("Connect failed (TUN routing): " + err.Error())
			})
			return
		}
		fyne.Do(func() {
			a.tunnel = t
			a.connected = target
			a.connBtn.SetText("Disconnect")
			a.list.Refresh()
			a.setBusy(false)
			a.setStatus(fmt.Sprintf("Connected (system-wide) via %s.", c.Name))
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

// indexOfLink returns the index of the config with the given link, or -1.
// An empty link always returns -1.
func indexOfLink(configs []model.Config, link string) int {
	if link == "" {
		return -1
	}
	for i := range configs {
		if configs[i].Link == link {
			return i
		}
	}
	return -1
}

func (a *App) setStatus(s string) { a.status.SetText(s) }

func (a *App) setBusy(b bool) { a.busy = b }

// persist saves the current list best-effort; a failure only surfaces in the
// status line and never blocks the action that triggered it.
func (a *App) persist() {
	if err := store.Save(a.all); err != nil {
		a.setStatus("Warning: could not save configs: " + err.Error())
	}
}
