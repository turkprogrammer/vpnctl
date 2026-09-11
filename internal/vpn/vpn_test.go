package vpn

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vpnctl/internal/priv"
)

// stubCommand replaces priv.Command with a recording fake and restores it after
// the test returns.
func stubCommand(t *testing.T, fn func(name string, args ...string) ([]byte, error)) *[][]string {
	t.Helper()
	var calls [][]string
	orig := priv.Command
	priv.Command = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return fn(name, args...)
	}
	t.Cleanup(func() { priv.Command = orig })
	return &calls
}

// setNetClassDir points interface detection at a temp dir for the duration of
// the test.
func setNetClassDir(t *testing.T, dir string) {
	t.Helper()
	orig := netClassDir
	netClassDir = dir
	t.Cleanup(func() { netClassDir = orig })
}

// setProbeURLs points the probe at the given URLs and restores the originals
// afterwards.
func setProbeURLs(t *testing.T, urls ...string) {
	t.Helper()
	origURLs := probeURLs
	origCheck := IPCheckURL
	probeURLs = urls
	if len(urls) > 0 {
		IPCheckURL = urls[0]
	}
	t.Cleanup(func() {
		probeURLs = origURLs
		IPCheckURL = origCheck
	})
}

func okCommand(name string, args ...string) ([]byte, error) {
	return nil, nil
}

func ipServer(t *testing.T, ip string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(ip))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func failServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", 500)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// hasCall reports whether calls contains the exact binary + args sequence.
func hasCall(calls [][]string, want []string) bool {
	for _, c := range calls {
		if len(c) != len(want) {
			continue
		}
		equal := true
		for i := range want {
			if c[i] != want[i] {
				equal = false
				break
			}
		}
		if equal {
			return true
		}
	}
	return false
}

func TestFetchIP(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr bool
	}{
		{"plain ip", 200, "203.0.113.7\n", "203.0.113.7", false},
		{"trace body", 200, "loc=US\nip=198.51.100.2\ntype=Wireguard\n", "198.51.100.2", false},
		{"first line without ip=", 200, "line1\nline2\n", "line1", false},
		{"non-200", 500, "boom", "", true},
		{"empty body", 200, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			got, err := fetchIP(context.Background(), srv.URL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("fetchIP = %v", err)
			}
			if got != tt.want {
				t.Errorf("fetchIP(%q) = %q, want %q", srv.URL, got, tt.want)
			}
		})
	}
}

func TestProbe(t *testing.T) {
	ip := ipServer(t, "203.0.113.9")
	fail := failServer(t)

	t.Run("first probe succeeds", func(t *testing.T) {
		setProbeURLs(t, ip.URL)
		got, err := probe(context.Background())
		if err != nil {
			t.Fatalf("probe = %v", err)
		}
		if got != "203.0.113.9" {
			t.Errorf("probe() = %q", got)
		}
	})

	t.Run("all probes fail returns last error", func(t *testing.T) {
		setProbeURLs(t, fail.URL)
		got, err := probe(context.Background())
		if err == nil {
			t.Fatal("want error")
		}
		if got != "" {
			t.Errorf("probe() = %q, want empty", got)
		}
	})

	t.Run("later probe wins after earlier failure", func(t *testing.T) {
		setProbeURLs(t, fail.URL, ip.URL)
		got, err := probe(context.Background())
		if err != nil {
			t.Fatalf("probe = %v", err)
		}
		if got != "203.0.113.9" {
			t.Errorf("probe() = %q", got)
		}
	})
}

func TestRemoteIP(t *testing.T) {
	t.Run("returns the probed IP", func(t *testing.T) {
		setProbeURLs(t, ipServer(t, "198.51.100.5").URL)
		if got := RemoteIP(); got != "198.51.100.5" {
			t.Errorf("RemoteIP = %q", got)
		}
	})

	t.Run("empty on failure", func(t *testing.T) {
		setProbeURLs(t, failServer(t).URL)
		if got := RemoteIP(); got != "" {
			t.Errorf("RemoteIP = %q, want empty", got)
		}
	})
}

func TestStatusAt(t *testing.T) {
	t.Run("interface up and IP filled", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "awg0"), 0o755); err != nil {
			t.Fatal(err)
		}
		setNetClassDir(t, dir)
		setProbeURLs(t, ipServer(t, "203.0.113.10").URL)

		st := (&Status{}).StatusAt()
		if st.Iface != "awg0" {
			t.Errorf("Iface = %q", st.Iface)
		}
		if !st.Up {
			t.Error("want Up = true")
		}
		if st.RemoteIP != "203.0.113.10" {
			t.Errorf("RemoteIP = %q", st.RemoteIP)
		}
	})

	t.Run("interface down", func(t *testing.T) {
		setNetClassDir(t, t.TempDir())
		st := (&Status{}).StatusAt()
		if st.Up {
			t.Error("want Up = false")
		}
	})

	t.Run("explicit interface is kept", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "custom0"), 0o755); err != nil {
			t.Fatal(err)
		}
		setNetClassDir(t, dir)
		st := (&Status{Iface: "custom0"}).StatusAt()
		if st.Iface != "custom0" {
			t.Errorf("Iface = %q", st.Iface)
		}
		if !st.Up {
			t.Error("want Up = true")
		}
	})
}

func TestUp(t *testing.T) {
	t.Run("already up — no privileged calls", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "awg0"), 0o755); err != nil {
			t.Fatal(err)
		}
		setNetClassDir(t, dir)
		calls := stubCommand(t, okCommand)

		if err := Up(context.Background(), "awg0", "", time.Second); err != nil {
			t.Fatalf("Up = %v", err)
		}
		if len(*calls) != 0 {
			t.Errorf("expected no privileged calls, got %v", *calls)
		}
	})

	t.Run("brings up and confirms internet", func(t *testing.T) {
		setNetClassDir(t, t.TempDir())
		setProbeURLs(t, ipServer(t, "198.51.100.8").URL)
		calls := stubCommand(t, okCommand)

		if err := Up(context.Background(), "awg0", "vpn.example:51820", time.Second); err != nil {
			t.Fatalf("Up = %v", err)
		}
		if !hasCall(*calls, []string{"sudo", "-n", "awg-quick", "up", "awg0"}) {
			t.Errorf("missing up call, got %v", *calls)
		}
	})

	t.Run("rolls back when no internet appears", func(t *testing.T) {
		setNetClassDir(t, t.TempDir())
		setProbeURLs(t, failServer(t).URL)
		calls := stubCommand(t, okCommand)

		err := Up(context.Background(), "awg0", "", 10*time.Millisecond)
		if err == nil {
			t.Fatal("want error")
		}
		if !hasCall(*calls, []string{"sudo", "-n", "awg-quick", "up", "awg0"}) {
			t.Errorf("missing up call, got %v", *calls)
		}
		if !hasCall(*calls, []string{"sudo", "-n", "awg-quick", "down", "awg0"}) {
			t.Errorf("missing rollback down call, got %v", *calls)
		}
	})

	t.Run("ctx cancellation triggers rollback", func(t *testing.T) {
		setNetClassDir(t, t.TempDir())
		setProbeURLs(t, failServer(t).URL)
		calls := stubCommand(t, okCommand)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := Up(ctx, "awg0", "", 10*time.Second)
		if err == nil {
			t.Fatal("want error")
		}
		if !strings.Contains(err.Error(), "откачено") {
			t.Errorf("error = %q, want rollback mention", err.Error())
		}
		if !hasCall(*calls, []string{"sudo", "-n", "awg-quick", "down", "awg0"}) {
			t.Errorf("missing rollback down call, got %v", *calls)
		}
	})

	t.Run("rollback failure is reported", func(t *testing.T) {
		setNetClassDir(t, t.TempDir())
		setProbeURLs(t, failServer(t).URL)
		downErr := errors.New("exit status 1")
		stubCommand(t, func(name string, args ...string) ([]byte, error) {
			for _, a := range args {
				if a == "down" {
					return []byte("denied"), downErr
				}
			}
			return nil, nil
		})

		err := Up(context.Background(), "awg0", "", 10*time.Millisecond)
		if err == nil {
			t.Fatal("want error")
		}
		if !strings.Contains(err.Error(), "сбой отката") {
			t.Errorf("error = %q, want rollback failure mention", err.Error())
		}
	})

	t.Run("up command failure propagates", func(t *testing.T) {
		setNetClassDir(t, t.TempDir())
		stubCommand(t, func(name string, args ...string) ([]byte, error) {
			return []byte("denied"), errors.New("exit status 1")
		})

		err := Up(context.Background(), "awg0", "", 10*time.Millisecond)
		var pe *priv.Error
		if !errors.As(err, &pe) {
			t.Fatalf("want *priv.Error, got %T (%v)", err, err)
		}
	})
}

func TestDown(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		calls := stubCommand(t, okCommand)
		if err := Down("awg0"); err != nil {
			t.Fatalf("Down = %v", err)
		}
		if !hasCall(*calls, []string{"sudo", "-n", "awg-quick", "down", "awg0"}) {
			t.Errorf("calls = %v", *calls)
		}
	})

	t.Run("failure propagates *priv.Error", func(t *testing.T) {
		stubCommand(t, func(name string, args ...string) ([]byte, error) {
			return []byte("denied"), errors.New("exit status 1")
		})
		err := Down("awg0")
		var pe *priv.Error
		if !errors.As(err, &pe) {
			t.Fatalf("want *priv.Error, got %T (%v)", err, err)
		}
	})
}

func TestError(t *testing.T) {
	if got := (&Error{Msg: "boom"}).Error(); got != "boom" {
		t.Errorf("Error() = %q", got)
	}
}
