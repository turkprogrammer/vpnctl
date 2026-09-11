// Package vpn exposes the two things the whole app talks about: turning the
// tunnel up/down and reading its status. Both the CLI (main.go) and the GUI
// (internal/ui) share exactly these functions (DRY). Privileged actions go
// through priv.
package vpn

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

// Error carries a pre-formatted, user-facing message produced when a tunnel
// cannot be brought up (see upRollback). Callers can display Msg as-is; it is
// not an errors.Is sentinel and does not wrap an underlying error.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

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

// netClassDir is the sysfs directory holding network interfaces. Overridable
// in tests via a temp dir.
var netClassDir = "/sys/class/net"

// ifaceUp reports whether the netdev for iface exists.
func ifaceUp(iface string) bool {
	_, err := os.Stat(filepath.Join(netClassDir, iface))
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

// ipClient fetches the probe URLs with a fresh connection every time
// (DisableKeepAlives), so a request never reuses a keep-alive socket from
// before the tunnel came up. One shared client is safe: without keep-alives
// no connection is ever reused across requests.
var ipClient = &http.Client{
	Timeout:   3 * time.Second,
	Transport: &http.Transport{DisableKeepAlives: true},
}

// fetchIP asks one URL for the external address, forcing a new connection.
func fetchIP(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := ipClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil {
		return "", fmt.Errorf("%s: чтение ответа: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
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
func probe(ctx context.Context) (string, error) {
	var last error
	for _, u := range probeURLs {
		ip, err := fetchIP(ctx, u)
		if err == nil {
			return ip, nil
		}
		last = err
	}
	return "", last
}

// RemoteIP returns the current external IP, or "" on any failure.
func RemoteIP() string {
	ip, _ := probe(context.Background())
	return ip
}

// Up safely brings the interface up: runs awg-quick up, waits for connectivity
// through the tunnel, and rolls back if none appears. If the interface is
// already up (e.g. turned on earlier via the terminal), it is left untouched —
// no need to bounce the existing tunnel.
//
// ctx controls the connectivity wait: cancelling it stops probing and tears
// the tunnel back down immediately. timeout bounds the wait when ctx never
// cancels.
//
// There is deliberately no pre-flight reachability probe: AmneziaWG/WireGuard
// peers speak UDP, so a TCP connect to the endpoint says nothing about whether
// the tunnel will work. The only honest check is "did internet appear after
// up", and that is what decides.
func Up(ctx context.Context, iface, endpoint string, timeout time.Duration) error {
	if ifaceUp(iface) {
		return nil
	}
	if _, err := priv.RunWG("awg-quick", "up", iface); err != nil {
		return err
	}

	// Wait for internet through the tunnel, stopping early if ctx is cancelled.
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	var lastErr error
	for {
		if ip, err := probe(ctx); ip != "" {
			return nil
		} else {
			lastErr = err
		}

		select {
		case <-ctx.Done():
			return upRollback(iface, timeout, endpoint, lastErr, "операция отменена")
		case <-deadline.C:
			return upRollback(iface, timeout, endpoint, lastErr, "истёк период ожидания")
		case <-tick.C:
		}
	}
}

// upRollback tears the interface down after connectivity never appeared and
// reports both the failure and whether the rollback itself succeeded.
func upRollback(iface string, timeout time.Duration, endpoint string, lastErr error, why string) error {
	where := endpoint
	if where == "" {
		where = "неизвестен"
	}
	msg := fmt.Sprintf(
		"интерфейс %s поднят, но за %s интернет через туннель не появился (%s) — соединение откачено (сервер %s; проверьте %s.conf; последняя ошибка проверки: %v)",
		iface, timeout, why, where, iface, lastErr)
	if _, err := priv.RunWG("awg-quick", "down", iface); err != nil {
		msg += "; сбой отката: " + err.Error()
	}
	return &Error{Msg: msg}
}

// Down brings the interface down.
func Down(iface string) error {
	_, err := priv.RunWG("awg-quick", "down", iface)
	return err
}
