// Command vpnctl — a small, local AmneziaWG/WireGuard manager for Ubuntu.
//
// Design: KISS, YAGNI, DRY. Privileged actions (up/down/import) all go through
// priv.Run (pkexec); status checks that need no privileges run as the user. The
// GUI (future) will call the exact same functions below.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"vpnctl/internal/conf"
	"vpnctl/internal/ui"
	"vpnctl/internal/vpn"
)

const (
	defIface   = "awg0"
	upTimeout  = 20 * time.Second
	usageIntro = `vpnctl — менеджер AmneziaWG/WireGuard

Использование:
  vpnctl status [iface]            показать статус туннеля
  vpnctl up [iface]                безопасно поднять VPN
  vpnctl down [iface]              опустить VPN
  vpnctl import <файл.conf>        импортировать конфиг в систему
  vpnctl tray                      графический интерфейс (окно + трей)
`
)

func main() {
	flag.Usage = func() { fmt.Fprint(os.Stderr, usageIntro) }
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	switch args[0] {
	case "status":
		cmdStatus(ifaceOf(args))
	case "up":
		cmdUp(ifaceOf(args))
	case "down":
		cmdDown(ifaceOf(args))
	case "import":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "ошибка: нужен путь к .conf")
			os.Exit(2)
		}
		cmdImport(args[1])
	case "tray", "ui", "gui":
		if err := ui.Run(); err != nil {
			fail(err)
		}
	default:
		fmt.Fprintf(os.Stderr, "ошибка: неизвестная команда %q\n", args[0])
		flag.Usage()
		os.Exit(2)
	}
}

func ifaceOf(args []string) string {
	if len(args) > 1 && args[1] != "" {
		return args[1]
	}
	return defIface
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "ошибка:", err)
	os.Exit(1)
}

func cmdStatus(iface string) {
	s := (&vpn.Status{Iface: iface}).StatusAt()
	state := "выключен"
	if s.Up {
		state = "включен"
	}
	fmt.Printf("интерфейс: %s  (%s)\n", s.Iface, state)
	if s.RemoteIP != "" {
		fmt.Printf("внешний IP: %s\n", s.RemoteIP)
	}
}

func cmdUp(iface string) {
	// ep stays empty when the endpoint can't be read without a prompt; it is
	// only used for display here. awg-quick up reads the config itself.
	ep := conf.InstalledEndpoint(iface)
	shown := ep
	if shown == "" {
		shown = "(неизвестен)"
	}
	fmt.Printf("поднимаю %s (сервер %s)...\n", iface, shown)
	if err := vpn.Up(iface, ep, upTimeout); err != nil {
		fail(err)
	}
	fmt.Println("VPN поднят.")
}

func cmdDown(iface string) {
	fmt.Printf("опускаю %s...\n", iface)
	if err := vpn.Down(iface); err != nil {
		fail(err)
	}
	fmt.Println("VPN опущен.")
}

func cmdImport(path string) {
	info, err := conf.Validate(path)
	if err != nil {
		fail(err)
	}
	if err := conf.Import(path); err != nil {
		if errors.Is(err, conf.ErrExists) {
			fmt.Printf("конфиг уже установлен (%s.conf) — повторно загружать не нужно.\n", info.Interface)
			fmt.Printf("подключение: vpnctl up %s\n", info.Interface)
			return
		}
		fail(err)
	}
	fmt.Printf("импортировано: %s (interface=%s, Endpoint=%s)\n", path, info.Interface, info.Endpoint)
	fmt.Printf("подключение: vpnctl up %s\n", info.Interface)
}
