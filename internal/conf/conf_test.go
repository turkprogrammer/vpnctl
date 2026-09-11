package conf

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vpnctl/internal/priv"
)

// stubCommand replaces priv.Command with a recording fake and restores it after
// the test returns.
func stubCommand(calls *[][]string, fn func(name string, args ...string) ([]byte, error)) func() {
	orig := priv.Command
	priv.Command = func(name string, args ...string) ([]byte, error) {
		*calls = append(*calls, append([]string{name}, args...))
		return fn(name, args...)
	}
	return func() { priv.Command = orig }
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInterfaceName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"WARPv2_54.conf", "warpv2_54"},
		{"WARPv2_54", "warpv2_54"},
		{"My Config.conf", "my_config"},
		{"a.b.conf", "a_b"},
		{"a.conf", "a"},
		{"aB9-_.conf", "ab9-_"},
		{"abcdefghijklmnop.conf", "awg0"},
		{"", "_"},
		{".conf", "awg0"},
		{"СЕКРЕТ.conf", "______"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InterfaceName(tt.name); got != tt.want {
				t.Errorf("InterfaceName(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	valid := "[Interface]\n" +
		"PrivateKey = abc\n" +
		"Address = 10.0.0.2/24\n" +
		"\n" +
		"[Peer]\n" +
		"PublicKey = xyz\n" +
		"Endpoint = 1.2.3.4:51820\n" +
		"AllowedIPs = 0.0.0.0/0\n"

	tests := []struct {
		name         string
		filename     string
		content      string
		wantEndpoint string
		wantErr      bool
	}{
		{"valid", "WARPv2_54.conf", valid, "1.2.3.4:51820", false},
		{"sections without endpoint", "a.conf", "[Interface]\n[Peer]\n", "не указан", false},
		{"endpoint on non-first line", "a.conf", "[Interface]\nPrivateKey = k\n\n[Peer]\nPublicKey = p\nEndpoint = 8.8.8.8:443\n", "8.8.8.8:443", false},
		{"empty file", "a.conf", "", "", true},
		{"missing peer", "a.conf", "[Interface]\n", "", true},
		{"missing interface", "a.conf", "[Peer]\n", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, t.TempDir(), tt.filename, tt.content)
			info, err := Validate(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate = %v", err)
			}
			if info.Interface == "" {
				t.Error("info.Interface is empty")
			}
			if info.Interface != InterfaceName(tt.filename) {
				t.Errorf("info.Interface = %q, want %q", info.Interface, InterfaceName(tt.filename))
			}
			if info.Endpoint != tt.wantEndpoint {
				t.Errorf("info.Endpoint = %q, want %q", info.Endpoint, tt.wantEndpoint)
			}
		})
	}

	t.Run("oversized file", func(t *testing.T) {
		path := writeFile(t, t.TempDir(), "big.conf", strings.Repeat("a", 1<<20+1))
		if _, err := Validate(path); err == nil {
			t.Fatal("want error for file over 1 MiB")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		if _, err := Validate(filepath.Join(t.TempDir(), "nope.conf")); err == nil {
			t.Fatal("want error for missing file")
		}
	})
}

func TestShquote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"abc", "'abc'"},
		{"a b", "'a b'"},
		{"a'b", "'a'\\''b'"},
		{"", "''"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := shquote(tt.in); got != tt.want {
				t.Errorf("shquote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestImport(t *testing.T) {
	confDir := filepath.Join(t.TempDir(), "conf")
	t.Setenv("VPNCTL_CONF_DIR", confDir)
	src := writeFile(t, t.TempDir(), "Work.conf", "[Interface]\n[Peer]\n")

	t.Run("success installs in a single sh -c call", func(t *testing.T) {
		var calls [][]string
		restore := stubCommand(&calls, func(name string, args ...string) ([]byte, error) {
			return nil, nil
		})
		defer restore()

		if err := Import(src); err != nil {
			t.Fatalf("Import = %v", err)
		}
		if len(calls) != 1 {
			t.Fatalf("expected a single privileged call, got %v", calls)
		}
		script := strings.Join(calls[0][1:], " ")
		for _, step := range []string{"mkdir -p", "install", "mv", "rm -f"} {
			if !strings.Contains(script, step) {
				t.Errorf("script missing %q: %s", step, script)
			}
		}
	})

	t.Run("installed config reports ErrExists", func(t *testing.T) {
		var calls [][]string
		restore := stubCommand(&calls, func(name string, args ...string) ([]byte, error) {
			return []byte("EXISTS"), errors.New("exit status 2")
		})
		defer restore()

		err := Import(src)
		if !errors.Is(err, ErrExists) {
			t.Fatalf("want ErrExists, got %v", err)
		}
	})

	t.Run("privileged failure is propagated", func(t *testing.T) {
		var calls [][]string
		restore := stubCommand(&calls, func(name string, args ...string) ([]byte, error) {
			return []byte("permission denied"), errors.New("exit status 1")
		})
		defer restore()

		if err := Import(src); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("invalid config never escalates", func(t *testing.T) {
		bad := writeFile(t, t.TempDir(), "bad.conf", "not a config")
		var calls [][]string
		restore := stubCommand(&calls, func(name string, args ...string) ([]byte, error) {
			return nil, nil
		})
		defer restore()

		if err := Import(bad); err == nil {
			t.Fatal("want error")
		}
		if len(calls) != 0 {
			t.Errorf("expected no privileged calls, got %v", calls)
		}
	})
}

func TestInstalledEndpoint(t *testing.T) {
	t.Run("endpoint parsed from strip output", func(t *testing.T) {
		var calls [][]string
		restore := stubCommand(&calls, func(name string, args ...string) ([]byte, error) {
			return []byte("[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nEndpoint = user@9.9.9.9:443\n"), nil
		})
		defer restore()

		if got := InstalledEndpoint("awg0"); got != "user@9.9.9.9:443" {
			t.Errorf("InstalledEndpoint = %q", got)
		}
	})

	t.Run("missing endpoint yields empty string", func(t *testing.T) {
		var calls [][]string
		restore := stubCommand(&calls, func(name string, args ...string) ([]byte, error) {
			return []byte("[Peer]\nPublicKey = z\n"), nil
		})
		defer restore()

		if got := InstalledEndpoint("awg0"); got != "" {
			t.Errorf("InstalledEndpoint = %q, want empty", got)
		}
	})

	t.Run("privileged failure yields empty string", func(t *testing.T) {
		var calls [][]string
		restore := stubCommand(&calls, func(name string, args ...string) ([]byte, error) {
			return nil, errors.New("no permissions")
		})
		defer restore()

		if got := InstalledEndpoint("awg0"); got != "" {
			t.Errorf("InstalledEndpoint = %q, want empty", got)
		}
	})
}

func FuzzInterfaceName(f *testing.F) {
	for _, s := range []string{"WARPv2_54.conf", "a b.conf", "", "abc"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, path string) {
		got := InterfaceName(path)
		if got == "" {
			t.Fatalf("InterfaceName(%q) returned empty", path)
		}
		if got != "awg0" && !ifaceRe.MatchString(got) {
			t.Errorf("InterfaceName(%q) = %q, does not match ifaceRe and is not awg0", path, got)
		}
	})
}

func FuzzValidate(f *testing.F) {
	f.Add("garbage input")
	f.Add("[Interface]\nPrivateKey = a\n\n[Peer]\nPublicKey = b\nEndpoint = 1.2.3.4:51820\n")
	f.Add("")
	f.Fuzz(func(t *testing.T, content string) {
		path := writeFile(t, t.TempDir(), "input.conf", content)
		info, err := Validate(path)
		if err != nil {
			return
		}
		if info.Interface != "awg0" && !ifaceRe.MatchString(info.Interface) {
			t.Errorf("Validate(...).Interface = %q, does not match ifaceRe", info.Interface)
		}
	})
}
