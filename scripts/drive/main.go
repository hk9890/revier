// Command drive runs the core by hand against a private hc core.
//
// It drives the core with no configuration: it builds a project in memory,
// runs run-or-raise against it, and prints the survey as JSON. Everything
// happens on an hc core of its own, in a directory of its own, so it never
// touches the user's hc core or service manager and never opens a window on
// any display: there is no view, as over ssh.
//
// Usage:
//
//	go run ./scripts/drive survey
//	go run ./scripts/drive go <target>
//	go run ./scripts/drive kill
//
// docs/RUNNING.md is the operating guide.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/hk9890/revier/internal/adapter/hc"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

func demoProject() revier.Project {
	sleep := []string{"sh", "-c", "sleep 3600"}
	return revier.Project{
		Name: "demo",
		Path: os.TempDir(),
		Targets: []revier.Target{
			{Name: "home", Key: "ctrl-shift-h", Home: true, Runtime: &revier.Realization{
				Name: "home", Launch: sleep, Match: revier.Match{Title: "^home$"},
			}},
			{Name: "diff", Key: "ctrl-shift-d", Runtime: &revier.Realization{
				Name: "diff", Launch: sleep, Match: revier.Match{Title: "^diff$"},
			}},
			// Window-only: unavailable here, which is what a headless machine
			// reports for a target it cannot realize.
			{Name: "editor", Key: "ctrl-o", Window: &revier.Realization{
				Name: "editor", Launch: []string{"code"}, Match: revier.Match{Class: "^code$"},
			}},
		},
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: drive survey | go <target> | kill")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dir := filepath.Join(os.TempDir(), fmt.Sprintf("revier-drive-%d", os.Getuid()))
	sock := private(dir)
	host := &hc.Host{Socket: sock}
	c := &core.Core{Runtime: host}
	project := core.PrepareProject(demoProject())

	switch os.Args[1] {
	case "survey":
		report, err := c.Survey(ctx, []core.Project{project})
		fatal(err)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		fatal(enc.Encode(report.Views))

	case "go":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: drive go <target>")
			os.Exit(2)
		}
		_, res, err := c.ActivateWaiting(ctx, project, revier.TargetName(os.Args[2]), nil)
		fatal(err)
		fmt.Printf("focused %s (%s) on %s\n", res.Ref.Title, res.Ref.ID, res.Ref.Host)

	case "kill":
		// hc's own CLI starts a core when none answers, so it is asked to
		// stop only one that does.
		if conn, err := net.Dial("unix", sock); err == nil {
			_ = conn.Close()
			out, err := exec.CommandContext(ctx, "hc", "server", "stop").CombinedOutput()
			if err != nil {
				fatal(fmt.Errorf("hc server stop: %w: %s", err, out))
			}
		}
		fatal(os.RemoveAll(dir))
		fmt.Println("core stopped")

	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

// private points this process, and every program it starts, at an hc core
// of the driver's own in dir, and returns its socket. Its runtime directory
// and the unset bus reach no service manager, so the core the host starts
// runs detached rather than as the user's revier-hc unit; revier's state
// goes there too. It exits when a manager still answers.
func private(dir string) string {
	run := filepath.Join(dir, "run")
	fatal(os.MkdirAll(run, 0o700))
	sock := filepath.Join(dir, "s.sock")
	for name, value := range map[string]string{
		"XDG_CONFIG_HOME": dir, "XDG_STATE_HOME": dir, "XDG_RUNTIME_DIR": run,
		"HC_SOCKET": sock, "SHELL": "/bin/sh", "REVIER_STATE_HOME": filepath.Join(dir, "revier"),
	} {
		fatal(os.Setenv(name, value))
	}
	for _, name := range []string{"DBUS_SESSION_BUS_ADDRESS", "HC_SESSION", "HC_TERMINAL_ID", "HC_TERMINAL_SOCKET", "HC_SCOPE"} {
		fatal(os.Unsetenv(name))
	}
	if exec.Command("systemctl", "--user", "--quiet", "show-environment").Run() == nil {
		fatal(fmt.Errorf("systemctl --user still reaches a service manager; the core would start in the user's own"))
	}
	return sock
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "drive:", err)
		os.Exit(1)
	}
}
