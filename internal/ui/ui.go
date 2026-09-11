// Package ui is the desktop front-end for vpnctl: one GTK3 application with a
// window hosting the config upload form, the on/off toggle, and live status
// monitoring.
//
// It reuses the exact same core logic as the CLI (vpn.Up/Down/Status,
// conf.Validate/Import) — no duplication (DRY).
package ui

import (
	"sync"
	"time"

	"github.com/gotk3/gotk3/gtk"
)

const (
	defaultIface = "awg0"
	pollEvery    = 3 // seconds
	upTimeout    = 20 * time.Second
	winW         = 420
	winH         = 360
)

// App holds the widgets we poke from goroutines.
type App struct {
	toggle     *gtk.ToggleButton
	stateLbl   *gtk.Label
	stateDot   *gtk.Label
	ifaceBadge *gtk.Label
	ipLbl      *gtk.Label
	srvLbl     *gtk.Label
	eventLbl   *gtk.Label
	importBtn  *gtk.Button
	fileBtn    *gtk.FileChooserButton
	busy       bool
	syncing    bool

	// mu guards the fields below: the interface the toggle and status act on
	// (it follows the config most recently imported this session, defaulting
	// to defaultIface) and the endpoint cached for it. The monitor goroutine
	// reads them while the import callback (main loop) may switch the
	// interface.
	mu       sync.Mutex
	iface    string
	endpoint string
	epFor    string
}

// currentIface returns the interface the toggle/status operate on.
func (a *App) currentIface() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.iface == "" {
		return defaultIface
	}
	return a.iface
}

// setIface switches the toggle/status to iface (after a successful import or
// when the config was already installed) and drops the endpoint cached for the
// previous one.
func (a *App) setIface(iface string) {
	a.mu.Lock()
	if a.iface != iface {
		a.endpoint, a.epFor = "", ""
	}
	a.iface = iface
	a.mu.Unlock()
}

// endpointResolved reports whether the endpoint for iface has already been
// looked up — including when the lookup came back empty, so an unreadable
// endpoint is not retried on every poll.
func (a *App) endpointResolved(iface string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.epFor == iface
}

// setEndpoint caches the endpoint found for iface.
func (a *App) setEndpoint(iface, endpoint string) {
	a.mu.Lock()
	a.epFor, a.endpoint = iface, endpoint
	a.mu.Unlock()
}

// currentEndpoint returns the cached endpoint for the active interface, or ""
// when it is unknown.
func (a *App) currentEndpoint() string {
	iface := a.currentIface()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.epFor != iface {
		return ""
	}
	return a.endpoint
}

// Run builds the window, then enters the GTK main loop.
func Run() error {
	gtk.Init(nil)
	if err := applyTheme(); err != nil {
		return err
	}
	a := &App{}
	a.build()
	a.refresh()
	go a.monitor()
	gtk.Main()
	return nil
}

func (a *App) build() {
	win, _ := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
	win.SetTitle("AmneziaWG VPN")
	win.SetDefaultSize(winW, winH)
	win.SetResizable(true)
	win.Add(a.buildContent())
	win.Connect("destroy", func() { gtk.MainQuit() })
	win.ShowAll()
}

// buildContent assembles the window body.
func (a *App) buildContent() *gtk.Box {
	root, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 12)
	root.SetBorderWidth(18)
	cls(root, "root")

	// --- header: icon, title, interface badge ---
	head, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	icon, _ := gtk.ImageNewFromIconName("network-vpn-symbolic", gtk.ICON_SIZE_BUTTON)
	head.PackStart(icon, false, false, 0)
	title, _ := gtk.LabelNew("AmneziaWG VPN")
	title.SetHAlign(gtk.ALIGN_START)
	cls(title, "apptitle")
	head.PackStart(title, true, true, 0)
	a.ifaceBadge, _ = gtk.LabelNew(defaultIface)
	cls(a.ifaceBadge, "badge")
	head.PackEnd(a.ifaceBadge, false, false, 0)
	root.PackStart(head, false, false, 0)

	// --- status card ---
	card, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 6)
	cls(card, "card")

	line, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 10)
	a.stateDot, _ = gtk.LabelNew("●")
	cls(a.stateDot, "dot")
	line.PackStart(a.stateDot, false, false, 0)
	a.stateLbl, _ = gtk.LabelNew("")
	a.stateLbl.SetHAlign(gtk.ALIGN_START)
	cls(a.stateLbl, "state")
	line.PackStart(a.stateLbl, true, true, 0)
	card.PackStart(line, false, false, 0)

	a.ipLbl, _ = gtk.LabelNew("")
	a.ipLbl.SetHAlign(gtk.ALIGN_START)
	a.ipLbl.SetSelectable(true)
	cls(a.ipLbl, "kvv")
	card.PackStart(a.ipLbl, false, false, 0)

	a.srvLbl, _ = gtk.LabelNew("")
	a.srvLbl.SetHAlign(gtk.ALIGN_START)
	cls(a.srvLbl, "kv")
	card.PackStart(a.srvLbl, false, false, 0)

	root.PackStart(card, false, false, 0)

	// --- config import ---
	sec, _ := gtk.LabelNew("КОНФИГУРАЦИЯ")
	sec.SetHAlign(gtk.ALIGN_START)
	cls(sec, "section")
	root.PackStart(sec, false, false, 0)

	row, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	fb, _ := gtk.FileChooserButtonNew("Выбрать конфиг…", gtk.FILE_CHOOSER_ACTION_OPEN)
	filter, _ := gtk.FileFilterNew()
	filter.AddPattern("*.conf")
	fb.AddFilter(filter)
	cls(fb, "chooser")
	a.fileBtn = fb
	row.PackStart(fb, true, true, 0)

	ib, _ := gtk.ButtonNewWithLabel("Загрузить")
	cls(ib, "ghost")
	a.importBtn = ib
	ib.Connect("clicked", a.onImport)
	row.PackStart(ib, false, false, 0)
	root.PackStart(row, false, false, 0)

	// --- primary action ---
	tg, _ := gtk.ToggleButtonNewWithLabel("ВКЛЮЧИТЬ VPN")
	cls(tg, "primary")
	a.toggle = tg
	tg.Connect("toggled", a.onToggle)
	root.PackStart(tg, false, false, 0)

	a.eventLbl, _ = gtk.LabelNew("")
	a.eventLbl.SetHAlign(gtk.ALIGN_START)
	a.eventLbl.SetLineWrap(true)
	a.eventLbl.SetSelectable(true)
	cls(a.eventLbl, "log")
	root.PackStart(a.eventLbl, false, false, 0)

	return root
}
