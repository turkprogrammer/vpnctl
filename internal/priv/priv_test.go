package priv

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// stubCommand replaces Command with a recording fake and restores it after the
// test returns.
func stubCommand(t *testing.T, fn func(name string, args ...string) ([]byte, error)) *[][]string {
	t.Helper()
	var calls [][]string
	orig := Command
	Command = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return fn(name, args...)
	}
	t.Cleanup(func() { Command = orig })
	return &calls
}

func TestRun(t *testing.T) {
	t.Run("success wraps command in pkexec", func(t *testing.T) {
		calls := stubCommand(t, func(name string, args ...string) ([]byte, error) {
			return []byte("ok"), nil
		})
		out, err := Run("awg-quick", "up", "awg0")
		if err != nil {
			t.Fatalf("Run = %v", err)
		}
		if string(out) != "ok" {
			t.Errorf("out = %q, want %q", out, "ok")
		}
		want := []string{"pkexec", "awg-quick", "up", "awg0"}
		if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
			t.Errorf("calls = %v, want %v", *calls, want)
		}
	})

	t.Run("failure is wrapped in *Error", func(t *testing.T) {
		stubCommand(t, func(name string, args ...string) ([]byte, error) {
			return []byte("denied"), errors.New("exit status 1")
		})
		out, err := Run("import")
		if err == nil {
			t.Fatal("want error")
		}
		var pe *Error
		if !errors.As(err, &pe) {
			t.Fatalf("want *Error, got %T", err)
		}
		if pe.Mode != "pkexec" || pe.Cmd != "import" || pe.Out != "denied" {
			t.Errorf("pe = %+v", pe)
		}
		if string(out) != "denied" {
			t.Errorf("out = %q", out)
		}
	})
}

func TestSudo(t *testing.T) {
	calls := stubCommand(t, func(name string, args ...string) ([]byte, error) {
		return []byte("x"), nil
	})
	out, err := sudo("strip", "awg0")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "x" {
		t.Errorf("out = %q", out)
	}
	want := []string{"sudo", "-n", "strip", "awg0"}
	if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
}

func TestRunWG(t *testing.T) {
	t.Run("sudo succeeds without pkexec", func(t *testing.T) {
		calls := stubCommand(t, func(name string, args ...string) ([]byte, error) {
			return []byte("ok"), nil
		})
		out, err := RunWG("awg-quick", "down", "awg0")
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != "ok" {
			t.Errorf("out = %q", out)
		}
		want := []string{"sudo", "-n", "awg-quick", "down", "awg0"}
		if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
			t.Errorf("calls = %v, want %v", *calls, want)
		}
	})

	t.Run("falls back to pkexec when sudo fails", func(t *testing.T) {
		calls := stubCommand(t, func(name string, args ...string) ([]byte, error) {
			if name == "sudo" {
				return []byte("sudo: a password is required"), errors.New("exit status 1")
			}
			return []byte("pkexec ok"), nil
		})
		out, err := RunWG("import")
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != "pkexec ok" {
			t.Errorf("out = %q", out)
		}
		if len(*calls) != 2 {
			t.Fatalf("expected 2 calls (sudo + pkexec), got %v", *calls)
		}
		if (*calls)[1][0] != "pkexec" {
			t.Errorf("second call binary = %q, want pkexec", (*calls)[1][0])
		}
	})

	t.Run("both fail reports the pkexec error", func(t *testing.T) {
		calls := stubCommand(t, func(name string, args ...string) ([]byte, error) {
			return []byte("nope"), errors.New("fail")
		})
		_, err := RunWG("import")
		var pe *Error
		if !errors.As(err, &pe) {
			t.Fatalf("want *Error, got %v", err)
		}
		if pe.Mode != "pkexec" {
			t.Errorf("mode = %q, want pkexec", pe.Mode)
		}
		if len(*calls) != 2 {
			t.Errorf("calls = %v", *calls)
		}
	})
}

func TestTryWG(t *testing.T) {
	calls := stubCommand(t, func(name string, args ...string) ([]byte, error) {
		return []byte("v"), nil
	})
	out, err := TryWG("awg-quick", "strip", "awg0")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "v" {
		t.Errorf("out = %q", out)
	}
	if len(*calls) != 1 || (*calls)[0][0] != "sudo" {
		t.Errorf("calls = %v, want a single sudo call", *calls)
	}
}

func TestError_Error(t *testing.T) {
	e := &Error{Mode: "pkexec", Cmd: "import", Args: []string{"x.conf"}, Err: errors.New("boom"), Out: "err"}
	got := e.Error()
	for _, want := range []string{"pkexec", "import", "x.conf", "boom", "err"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
}
