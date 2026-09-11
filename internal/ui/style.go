package ui

import (
	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/gtk"
)

// themeCSS is the application's identity: a dark, terminal-flavoured surface
// with one blue for the primary action, green/grey for state, and monospace
// for addresses. It is deliberately self-contained — the window must look the
// same on a light desktop as on a dark one.
const themeCSS = `
window { background-color: #14161a; }

.root { background-color: #14161a; }

.apptitle {
  color: #f2f4f7;
  font-size: 16px;
  font-weight: 600;
}

.badge {
  font-family: monospace;
  font-size: 11px;
  background-color: #232830;
  color: #9aa4b2;
  border-radius: 4px;
  padding: 2px 8px;
}

.card {
  background-color: #1b1f26;
  border: 1px solid #272d37;
  border-radius: 10px;
  padding: 14px 16px;
}

.state { font-size: 20px; font-weight: 700; }
label.state.on  { color: #46d17a; }
label.state.off { color: #6b7480; }

.dot { font-size: 18px; }
label.dot.on  { color: #46d17a; }
label.dot.off { color: #4a525e; }

.kv  { font-family: monospace; font-size: 12px; color: #8b94a1; }
.kvv { font-family: monospace; font-size: 12px; color: #cfd6e0; }

.section { font-size: 11px; color: #6b7480; }

.log { font-size: 11px; color: #7a828e; }

.chooser button {
  background-image: none;
  background-color: #1e232a;
  color: #d5dae2;
  border: 1px solid #2f3742;
  border-radius: 8px;
  padding: 8px 12px;
  font-size: 13px;
}

button.primary {
  background-image: none;
  background-color: #2f6feb;
  color: #ffffff;
  border: none;
  border-radius: 8px;
  font-weight: 600;
  font-size: 14px;
  padding: 13px 16px;
}
button.primary:hover { background-color: #3b7cf5; }
button.primary:active { background-color: #2a61d0; }
button.primary:disabled { background-color: #2a3340; color: #6b7480; }

button.ghost {
  background-image: none;
  background-color: #232830;
  color: #d5dae2;
  border: 1px solid #2f3742;
  border-radius: 8px;
  padding: 9px 14px;
  font-size: 13px;
}
button.ghost:hover { background-color: #2a313b; }
button.ghost:active { background-color: #1e232a; }
button.ghost:disabled { background-color: #1e232a; color: #5a626d; }
`

// stylable is any widget whose CSS classes can be attached.
type stylable interface {
	GetStyleContext() (*gtk.StyleContext, error)
}

// cls attaches CSS classes — this is AddClass, matching ".name" selectors.
// Widget.SetName is a different selector (#name) and silently matches nothing
// here, so always style through this helper.
func cls(w stylable, names ...string) {
	sc, err := w.GetStyleContext()
	if err != nil {
		return
	}
	for _, n := range names {
		sc.AddClass(n)
	}
}

// setState paints a stateful label: its text plus the on/off class that
// colours it.
func setState(w *gtk.Label, text string, on bool) {
	w.SetText(text)
	sc, err := w.GetStyleContext()
	if err != nil {
		return
	}
	if on {
		sc.RemoveClass("off")
		sc.AddClass("on")
		return
	}
	sc.RemoveClass("on")
	sc.AddClass("off")
}

// applyTheme installs themeCSS for the whole screen and asks GTK for the dark
// variant, so the window keeps its identity regardless of the desktop setting.
func applyTheme() error {
	if s, err := gtk.SettingsGetDefault(); err == nil && s != nil {
		_ = s.SetProperty("gtk-application-prefer-dark-theme", true)
	}
	p, err := gtk.CssProviderNew()
	if err != nil {
		return err
	}
	if err := p.LoadFromData(themeCSS); err != nil {
		return err
	}
	screen, err := gdk.ScreenGetDefault()
	if err != nil {
		return err
	}
	gtk.AddProviderForScreen(screen, p, uint(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION))
	return nil
}
