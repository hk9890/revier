// Command drive runs the core by hand against a private tmux server.
//
// It is the manual-verification surface until the real CLI exists: it builds a
// project in memory, runs run-or-raise against it, and prints the survey as
// JSON. Everything happens on a tmux socket of its own, so it never touches the
// user's tmux sessions and never opens a window on any display.
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
	"os"
	"os/exec"
	"time"

	"github.com/hk9890/revier/internal/adapter/tmux"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// socket keeps every run of this program on one private server, so state
// survives between invocations and `kill` has something to remove.
const socket = "revier-drive"

func demoProject() revier.Project {
	sleep := []string{"sh", "-c", "sleep 3600"}
	return revier.Project{
		Name: "demo",
		Path: "/tmp/demo",
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

	host := &tmux.Host{Socket: socket, Session: "revier-drive"}
	c := &core.Core{Runtime: host}
	project, err := core.PrepareProject(demoProject())
	fatal(err)

	switch os.Args[1] {
	case "survey":
		views, err := c.Survey(ctx, []core.Project{project})
		fatal(err)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		fatal(enc.Encode(views))

	case "go":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: drive go <target>")
			os.Exit(2)
		}
		ref, err := c.Go(ctx, project, revier.TargetName(os.Args[2]))
		fatal(err)
		fmt.Printf("focused %s (%s) on %s\n", ref.Title, ref.ID, ref.Host)

	case "kill":
		// Errors are expected when no server is running.
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
		fmt.Println("server killed")

	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "drive:", err)
		os.Exit(1)
	}
}
