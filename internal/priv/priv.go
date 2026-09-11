// Package priv is the single place in the app that escalates privileges.
// Privileged operations (up/down/import/status) go through these helpers.
//
// Run    — uses pkexec (graphical password prompt). Used for import and as a
//
//	fallback when passwordless sudo is not available.
//
// RunWG  — tries passwordless sudo first (NOPASSWD), then falls back to
//
//	pkexec. Used for the awg-quick commands covered by a sudoers rule,
//	so up/down work without a password where that rule exists.
//
// TryWG  — best-effort passwordless sudo only, never shows a prompt. Used for
//
//	optional privilege reads that should silently be empty.
package priv

import (
	"fmt"
	"os/exec"
)

// Error carries context about a failed privileged command.
type Error struct {
	Mode string
	Cmd  string
	Args []string
	Err  error
	Out  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s %s %v: %v (вывод: %s)", e.Mode, e.Cmd, e.Args, e.Err, e.Out)
}

func wgErr(cmd, mode string, args []string, err error, out []byte) *Error {
	return &Error{Mode: mode, Cmd: cmd, Args: args, Err: err, Out: string(out)}
}

// Run executes name (as root) with args via pkexec (graphical prompt).
// Non-zero exit turns into a *Error with the captured output attached.
func Run(name string, args ...string) ([]byte, error) {
	cmd := exec.Command("pkexec", append([]string{name}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, wgErr(name, "pkexec", args, err, out)
	}
	return out, nil
}

// sudo tries a passwordless sudo (NOPASSWD); errs if it needs a password.
func sudo(name string, args ...string) ([]byte, error) {
	cmd := exec.Command("sudo", append([]string{"-n", name}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, wgErr(name, "sudo", args, err, out)
	}
	return out, nil
}

// RunWG runs name+args with privileges, preferring passwordless sudo (when a
// sudoers rule covers it) and falling back to pkexec. Good for awg-quick
// up/down so they need no password where a NOPASSWD rule exists.
func RunWG(name string, args ...string) ([]byte, error) {
	if out, err := sudo(name, args...); err == nil {
		return out, nil
	}
	return Run(name, args...)
}

// TryWG is a best-effort passwordless sudo that never prompts. It returns nil
// output+error on anything but a clean passwordless run — used for optional
// privilege reads that should just come back empty (e.g. handshake info).
func TryWG(name string, args ...string) ([]byte, error) {
	return sudo(name, args...)
}
