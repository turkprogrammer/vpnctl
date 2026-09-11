package ui

import (
	"context"
	"errors"
	"fmt"

	"github.com/gotk3/gotk3/glib"

	"vpnctl/internal/conf"
	"vpnctl/internal/vpn"
)

// onImport uploads the chosen .conf into the system (privileged, GUI pkexec
// prompt). Runs in a goroutine so the window stays responsive.
func (a *App) onImport() {
	if a.busy {
		return
	}
	path := a.fileBtn.GetFilename()
	if path == "" {
		a.logEvent("не выбран файл")
		return
	}
	a.busy = true
	a.importBtn.SetSensitive(false)
	a.logEvent("импорт " + path + "…")

	go func() {
		info, err := conf.Validate(path)
		if err != nil {
			a.reportImport(path, nil, err)
			return
		}
		a.reportImport(path, info, conf.Import(path))
	}()
}

// reportImport logs the outcome of an import on the main loop. On success, or
// when the config is already installed, it points the toggle at the imported
// interface so ВКЛ/ВЫКЛ act on the config the user just chose.
func (a *App) reportImport(path string, info *conf.Info, err error) {
	var msg string
	switch {
	case err == nil:
		msg = fmt.Sprintf("импортирован %s: интерфейс %s, сервер %s — нажмите ВКЛ VPN для подключения",
			path, info.Interface, info.Endpoint)
	case errors.Is(err, conf.ErrExists):
		msg = fmt.Sprintf("конфиг уже установлен (%s.conf) — повторно загружать не нужно, нажмите ВКЛ VPN",
			info.Interface)
	default:
		msg = "ошибка импорта: " + err.Error()
	}

	installed := err == nil || errors.Is(err, conf.ErrExists)
	_ = glib.IdleAdd(func() {
		a.busy = false
		a.importBtn.SetSensitive(true)
		if installed {
			a.setIface(info.Interface)
		}
		a.logEvent(msg)
	})
}

// onToggle brings the tunnel up or down. Runs in a goroutine; pkexec shows a
// graphical auth prompt.
func (a *App) onToggle() {
	// Ignore programmatic state sync from renderStatus (SetActive) and while
	// another operation is in flight.
	if a.busy || a.syncing {
		return
	}
	a.busy = true
	a.toggle.SetSensitive(false)
	on := a.toggle.GetActive()
	iface := a.currentIface()

	go func() {
		noun, gerund := "подключение", "подключения"
		var err error
		if on {
			ep := conf.InstalledEndpoint(iface)
			// Endpoint may be unreadable without extra privileges; Up only
			// pre-checks reachability when it has an endpoint to use.
			err = vpn.Up(context.Background(), iface, ep, upTimeout)
		} else {
			noun, gerund = "отключение", "отключения"
			err = vpn.Down(iface)
		}

		st := (&vpn.Status{Iface: iface}).StatusAt()
		_ = glib.IdleAdd(func() {
			a.busy = false
			a.toggle.SetSensitive(true)
			a.renderStatus(st)
			if err != nil {
				a.logEvent("ошибка " + gerund + " (" + iface + "): " + err.Error())
			} else {
				a.logEvent("успешно: " + noun + " (" + iface + ")")
			}
		})
	}()
}
