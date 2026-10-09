package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	usecase "github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// cmdNew writes a project file for the working directory. It needs no host,
// so it runs before any is probed: a new checkout on a machine with no
// terminal revier can drive still gets its project.
func cmdNew(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return fmt.Errorf("usage: revier new [name]")
	}
	root, err := config.Root()
	if err != nil {
		return err
	}
	cfg, projects, err := config.Load(root)
	if err != nil {
		return err
	}
	warnProblems(cfg, projects)
	name := ""
	if len(pos) > 0 {
		name = pos[0]
	}
	_, err = createProject(out, root, projects, revier.ProjectName(name))
	return err
}

// createProject writes the project for the working directory, as
// app.CreateProject writes it. An empty name is the directory's own.
func createProject(out io.Writer, root string, projects []core.Project, name revier.ProjectName) (core.Project, error) {
	dir, err := os.Getwd()
	if err != nil {
		return core.Project{}, err
	}
	p, err := usecase.CreateProject(root, projects, name, dir, "", os.Stderr)
	if err != nil {
		return core.Project{}, err
	}
	_, _ = fmt.Fprintf(out, "created %s\n", p.File)
	return p, nil
}
