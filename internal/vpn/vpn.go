// Package vpn exposes the two things the whole app talks about: turning the
// tunnel up/down and reading its status. Both the CLI (main.go) and the future
// GUI share exactly these functions (DRY). Privileged actions go through priv.
package vpn

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"vpnctl/internal/priv"
)

// IPCheckURL returns the externally-reachable IP. Overridable via env for tests.
var IPCheckURL = envOr("VPNCTL_IP_URL", "https://ifconfig.me/ip")

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Status is the single status record shared between CLI output and GUI fields.
type Status struct {
	Iface    string
	Up       bool
	RemoteIP string
}

// StatusAt fills s. Checking interface presence needs no privileges (sysfs),
// so it's safe to poll every few seconds. It deliberately does NOT read
// `awg show` here, because that requires privileges and would trigger a
// password prompt on every poll (the "annoying auth window" bug).
func (s *Status) StatusAt() *Status {
	s.Iface = s.ifaceName()
	s.Up = ifaceUp(s.Iface)
	s.RemoteIP = RemoteIP()
	return s
}

func (s *Status) ifaceName() string {
	if s.Iface != "" {
		return s.Iface
	}
	return "awg0"
}

// ifaceUp reports whether the netdev for iface exists.
func ifaceUp(iface string) bool {
	_, err := os.Stat("/sys/class/net/" + iface)
	return err == nil
}

// probeURLs are checked in order to decide whether the tunnel really carries
// traffic. Several hosts are used because a single one can be slow or blocked,
// and every request opens a fresh connection: a keep-alive socket from before
// the tunnel came up is dead, and reusing it would burn the whole timeout
// without ever testing the new route.
var probeURLs = []string{
	IPCheckURL,
	"https://api.ipify.org",
	"https://icanhazip.com",
	// Needs no DNS, so it still answers while the resolver is settling on the
	// tunnel; its body is a key=value trace rather than a bare IP.
	"https://1.1.1.1/cdn-cgi/trace",
}

// fetchIP asks one URL for the external address, forcing a new connection.
func fetchIP(url string) (string, error) {
	c := &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
	resp, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%s: http %d", url, resp.StatusCode)
	}
	txt := strings.TrimSpace(string(body))
	if txt == "" {
		return "", fmt.Errorf("%s: пустой ответ", url)
	}
	for _, line := range strings.Split(txt, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "ip="); ok {
			return strings.TrimSpace(v), nil
		}
	}
	return strings.SplitN(txt, "\n", 2)[0], nil
}

// probe returns the externally visible IP once any check succeeds, together
// with the last error when none did, so failures can be reported precisely.
func probe() (string, error) {
	var last error
	for _, u := range probeURLs {
		ip, err := fetchIP(u)
		if err == nil {
			return ip, nil
		}
		last = err
	}
	return "", last
}

// RemoteIP returns the current external IP, or "" on any failure.
func RemoteIP() string {
	ip, _ := probe()
	return ip
}

// Up safely brings the interface up: runs awg-quick up, waits for connectivity
// through the tunnel, and rolls back if none appears. If the interface is
// already up (e.g. turned on earlier via the terminal), it is left untouched —
// no need to bounce the existing tunnel.
//
// There is deliberately no pre-flight reachability probe: AmneziaWG/WireGuard
// peers speak UDP, so a TCP connect to the endpoint says nothing about whether
// the tunnel will work. The only honest check is "did internet appear after
// up", and that is what decides.
func Up(iface, endpoint string, timeout time.Duration) error {
	if ifaceUp(iface) {
		return nil
	}
	if _, err := priv.RunWG("awg-quick", "up", iface); err != nil {
		return err
	}

	// Wait for internet through the tunnel.
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if ip, err := probe(); ip != "" {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(time.Second)
	}
	// Rollback: restore original networking.
	_, _ = priv.RunWG("awg-quick", "down", iface)
	where := endpoint
	if where == "" {
		where = "неизвестен"
	}
	return &Error{Msg: fmt.Sprintf(
		"интерфейс %s поднят, но за %s интернет через туннель не появился — соединение откатено (сервер %s; проверьте %s.conf; последняя ошибка проверки: %v)",
		iface, timeout, where, iface, lastErr)}
}

// Down brings the interface down.
func Down(iface string) error {
	_, err := priv.RunWG("awg-quick", "down", iface)
	return err
}

// Error is a small user-facing error without extra wrapping.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }
