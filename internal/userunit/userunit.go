// Package userunit starts a long-lived program as a service of the user's
// service manager, so it has the session's login environment whichever
// process asks first: a program started from a shell keeps that shell's
// environment, a venv's or a credential's, and hands it to everything it
// starts for its whole life. A fixed list of variables is
// no answer: it drops each desktop variable revier does not know.
package userunit

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Unit is a service of the user's service manager.
type Unit struct {
	// Name is the unit's one name: a second start while it runs fails, so
	// two starts at once start one program, and the user stops it by it.
	Name string
	// Properties are systemd unit properties, as KEY=VALUE.
	Properties []string
	// Env is what the program is given beside the manager's environment, as
	// KEY=VALUE.
	Env []string
}

// Start runs argv as u through systemd-run. A start that fails because u runs
// already, or is starting, is one another process made, and no error: a
// forking unit is starting until its program has forked. Where there is no
// systemd-run, or it cannot start u, Start calls detached, which starts the
// program apart from the service manager.
//
// When systemd-run fails and u neither runs nor is starting, Start writes one
// WARN "the service manager did not start the unit; starting it detached",
// with unit and err. With no systemd-run it writes nothing. Its callers
// start the workspace kitty and the hc core.
func Start(ctx context.Context, u Unit, argv []string, detached func(context.Context) error) error {
	run, err := exec.LookPath("systemd-run")
	if err != nil {
		return detached(ctx)
	}
	args := []string{"--user", "--quiet", "--collect", "--unit=" + u.Name}
	for _, p := range u.Properties {
		args = append(args, "--property="+p)
	}
	for _, e := range u.Env {
		args = append(args, "--setenv="+e)
	}
	err = Run(ctx, run, append(append(args, "--"), argv...)...)
	if err == nil || running(ctx, u.Name) {
		return nil
	}
	slog.Warn("the service manager did not start the unit; starting it detached", "unit", u.Name, "err", err)
	return detached(ctx)
}

// running reports whether the service manager runs the unit name, or is
// starting it.
func running(ctx context.Context, name string) bool {
	// is-active exits non-zero for a unit that is starting, and prints its
	// state whatever it exits with.
	out, _ := exec.CommandContext(ctx, "systemctl", "--user", "is-active", name).Output()
	switch strings.TrimSpace(string(out)) {
	case "active", "activating", "reloading":
		return true
	}
	return false
}

// Outlives reports whether a unit Start starts outlives the user's last
// login session: the service manager stops with that session, and every unit
// in it, unless the user lingers. known is false where Start would start the
// program detached, as with no systemd-run or no service manager that
// answers, and where loginctl cannot say.
func Outlives(ctx context.Context) (lingers, known bool) {
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return false, false
	}
	if exec.CommandContext(ctx, "systemctl", "--user", "--quiet", "show-environment").Run() != nil {
		return false, false
	}
	out, err := exec.CommandContext(ctx, "loginctl", "show-user", strconv.Itoa(os.Getuid()), "--property=Linger", "--value").Output()
	if err != nil {
		return false, false
	}
	switch strings.TrimSpace(string(out)) {
	case "yes":
		return true, true
	case "no":
		return false, true
	}
	return false, false
}

// Run runs a command that starts a program, with no terminal to read or write,
// in a process group of its own: the ctrl+c a terminal sends to revier's group
// cancels a shutdown or a restore after its step, and must not end the step's
// command with it.
func Run(ctx context.Context, name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(name), err)
	}
	return nil
}
