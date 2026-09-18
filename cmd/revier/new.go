package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hk9890/revier/internal/checkout"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// cmdNew writes a project file for the working directory. It needs no host,
// so it runs before any is probed: a new checkout on a machine with no
// terminal revier can drive still gets its project.
func cmdNew(args []string) error {
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
	_, err = createProject(root, projects, revier.ProjectName(name))
	return err
}

// createProject writes the project for the working directory and trusts the
// directory's mise configuration, as `os open` does for a session it creates.
// An empty name is the directory's own. What config.CanCreate refuses is
// refused here.
func createProject(root string, projects []core.Project, name revier.ProjectName) (core.Project, error) {
	dir, err := os.Getwd()
	if err != nil {
		return core.Project{}, err
	}
	if name == "" {
		name = config.NameFor(dir)
	}
	if err := config.CanCreate(projects, name, dir); err != nil {
		return core.Project{}, err
	}
	p, err := config.Create(root, name, dir, checkout.Origin(dir))
	if err != nil {
		return core.Project{}, err
	}
	checkout.Trust(dir, os.Stderr)
	fmt.Printf("created %s\n", p.File)
	return p, nil
}
