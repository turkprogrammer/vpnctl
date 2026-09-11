package ui

import (
	"time"

	"github.com/gotk3/gotk3/glib"

	"vpnctl/internal/conf"
	"vpnctl/internal/vpn"
)

// monitor polls status off the main loop and pushes UI updates onto it. The
// first poll runs immediately so the window fills in without waiting a tick.
func (a *App) monitor() {
	a.poll()
	t := time.NewTicker(time.Duration(pollEvery) * time.Second)
	defer t.Stop()
	for range t.C {
		a.poll()
	}
}

// poll refreshes the cached endpoint (only once per interface, because the
// lookup is a privileged call) and reports the current status to the main
// loop. It runs off the main loop, so it never blocks the window.
func (a *App) poll() {
	iface := a.currentIface()
	if !a.endpointResolved(iface) {
		a.setEndpoint(iface, conf.InstalledEndpoint(iface))
	}
	st := (&vpn.Status{Iface: iface}).StatusAt()
	_ = glib.IdleAdd(func() { a.renderStatus(st) })
}

// refresh draws the initial, still-unknown state. It deliberately performs no
// I/O: the monitor's immediate first poll fills in the real status off the main
// loop, so a slow or unreachable network never delays the window appearing.
func (a *App) refresh() {
	a.renderStatus(&vpn.Status{Iface: a.currentIface()})
	a.logEvent("готов")
}

// renderStatus updates the window from a Status record (main loop).
func (a *App) renderStatus(s *vpn.Status) {
	// Reflect state on the toggle. Suppress the "toggled" signal that SetActive
	// would otherwise fire, so polling never triggers a real up/down (see
	// onToggle's syncing guard).
	a.syncing = true
	a.toggle.SetActive(s.Up)
	a.syncing = false

	if s.Up {
		a.toggle.SetLabel("ВЫКЛЮЧИТЬ VPN")
		setState(a.stateLbl, "ПОДКЛЮЧЕНО", true)
		setState(a.stateDot, "●", true)
	} else {
		a.toggle.SetLabel("ВКЛЮЧИТЬ VPN")
		setState(a.stateLbl, "ОТКЛЮЧЕНО", false)
		setState(a.stateDot, "●", false)
	}

	a.ifaceBadge.SetText(s.Iface)

	if s.RemoteIP != "" {
		a.ipLbl.SetText(s.RemoteIP)
	} else {
		a.ipLbl.SetText("—")
	}

	if ep := a.currentEndpoint(); ep != "" {
		a.srvLbl.SetText("сервер " + ep)
	} else {
		a.srvLbl.SetText("сервер неизвестен")
	}
}

func (a *App) logEvent(msg string) {
	a.eventLbl.SetText("Последнее: " + msg)
}
