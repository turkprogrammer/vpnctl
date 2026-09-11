// Package conf validates and imports a WireGuard/AmneziaWG .conf file into
// the system config directory. It never inspects secrets beyond what awg-quick
// itself does; it only checks shape and writes the file through priv.Run so the
// private key lives in a root-owned 0600 file.
package conf

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"vpnctl/internal/priv"
)

// SysDir is where AmneziaWG keeps interface configs. Overridable via
// VPNCTL_CONF_DIR for tests.
var SysDir = envOr("VPNCTL_CONF_DIR", "/etc/amnezia/amneziawg")

// ErrExists reports that the target config is already installed. Re-importing
// is neither needed nor done — a working config is never silently replaced.
var ErrExists = errors.New("конфиг уже установлен")

// Info summarizes a validated config.
type Info struct {
	Interface string
	Endpoint  string
}

var (
	ifaceRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,15}$`)
	epRe    = regexp.MustCompile(`(?m)^Endpoint\s*=\s*(.+)$`)
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// InterfaceName derives an interface name from a conf filename:
// "WARPv2_54.conf" -> "warpv2_54". Allowed characters are kept as-is (so "_"
// survives), everything else becomes "_"; the name is lowercased. Falls back
// to a fixed name if the result would be invalid.
func InterfaceName(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		default:
			return '_'
		}
	}, strings.ToLower(base))
	if !ifaceRe.MatchString(base) {
		return "awg0"
	}
	return base
}

// Validate reads path and checks it looks like a usable .conf. It returns
// Info (with a sane interface name) or an error explaining what's wrong.
func Validate(path string) (*Info, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать %s: %w", path, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("файл пуст")
	}
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("файл слишком большой")
	}

	body := string(data)
	for _, sec := range []string{"[Interface]", "[Peer]"} {
		if !strings.Contains(body, sec) {
			return nil, fmt.Errorf("нет секции %s", sec)
		}
	}

	info := &Info{Interface: InterfaceName(path)}
	if m := epRe.FindStringSubmatch(body); len(m) == 2 {
		info.Endpoint = strings.TrimSpace(m[1])
	} else {
		info.Endpoint = "не указан"
	}
	return info, nil
}

// InstalledEndpoint returns the Endpoint from the installed system config for
// iface, or "" if it can't be read. Read is best-effort via passwordless sudo
// (TryWG, never prompts) so that checking the endpoint before "up" doesn't
// pop an auth window — if it can't be read, "" is returned.
func InstalledEndpoint(iface string) string {
	// awg-quick strip prints the config without secrets and is covered by the
	// passwordless sudoers rule, so this reads the endpoint with no prompt.
	// TryWG never prompts: where the rule is absent, "" is returned and the
	// caller proceeds without a server name.
	out, err := priv.TryWG("awg-quick", "strip", iface)
	if err != nil {
		return ""
	}
	m := epRe.FindStringSubmatch(string(out))
	if len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// shquote quotes a string for POSIX shell: wraps in single quotes, escaping
// embedded single quotes. Used to build a safe privileged command for import.
func shquote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// Import copies path into SysDir/<iface>.conf as one privileged operation:
// refuse to overwrite an existing config, copy with owner root and mode 0600,
// then atomically move into place. Guard, install, mv and cleanup all run
// inside a single `sh -c`, so one import means at most ONE auth prompt and a
// failed import never leaves a temp file behind. An already-installed config
// is reported as ErrExists rather than replaced.
func Import(path string) error {
	if _, err := Validate(path); err != nil {
		return err
	}
	name := InterfaceName(path)
	dst := filepath.Join(SysDir, name+".conf")
	tmp := filepath.Join(SysDir, "."+name+".tmp")

	// Single root command: guard against overwrite, make sure the target dir
	// exists, install (copy + owner root + mode 0600) to a temp name, mv into
	// place atomically, and remove the temp file on any failure.
	script := "if [ -e " + shquote(dst) + " ]; then echo EXISTS; exit 2; fi; " +
		"mkdir -p " + shquote(SysDir) + " && " +
		"install -o root -g root -m 600 " + shquote(path) + " " + shquote(tmp) +
		" && mv " + shquote(tmp) + " " + shquote(dst) +
		" || { rm -f " + shquote(tmp) + "; exit 1; }"

	out, err := priv.RunWG("sh", "-c", script)
	if err != nil {
		if strings.Contains(string(out), "EXISTS") {
			return fmt.Errorf("%w: %s", ErrExists, dst)
		}
		return err
	}
	return nil
}
