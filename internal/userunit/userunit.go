// Package userunit starts a long-lived program as a service of the user's
// service manager, so it has the session's login environment whichever
// process asks first: a program started from a shell keeps that shell's
// environment, a venv's or a credential's, and hands it to everything it
// starts for its whole life (decisions.md D167).
package userunit

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
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
// already is one another process made, and no error. Where there is no
// systemd-run, or it cannot start u, Start calls detached, which starts the
// program apart from the service manager.
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
	if err == nil || Run(ctx, "systemctl", "--user", "--quiet", "is-active", u.Name) == nil {
		return nil
	}
	slog.Warn("the service manager did not start the unit; starting it detached", "unit", u.Name, "err", err)
	return detached(ctx)
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
