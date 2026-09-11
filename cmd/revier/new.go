package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

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
	_, projects, err := config.Load(root)
	if err != nil {
		return err
	}
	name := ""
	if len(pos) > 0 {
		name = pos[0]
	}
	_, err = createProject(root, projects, revier.ProjectName(name))
	return err
}

// createProject writes the project for the working directory and trusts the
// directory's mise configuration, as `os open` does for a session it creates.
// An empty name is the directory's own.
//
// Two directories are refused, and a name already taken. A directory that is
// already a project's path would become two projects fighting over it. The
// home directory would own every directory under it that no other project
// claims, so a keypress in any of them would resolve to it instead of
// reporting no project.
func createProject(root string, projects []core.Project, name revier.ProjectName) (core.Project, error) {
	dir, err := os.Getwd()
	if err != nil {
		return core.Project{}, err
	}
	if name == "" {
		name = config.NameFor(dir)
	}
	for _, p := range projects {
		if filepath.Clean(p.Path) == dir {
			return core.Project{}, fmt.Errorf("%s is already project %q", dir, p.Name)
		}
		// A file of another name can declare this project name, and the load
		// refuses two projects under one name.
		if p.Name == name {
			return core.Project{}, fmt.Errorf("project %q already exists: %s", name, p.File)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(home) == dir {
		return core.Project{}, fmt.Errorf("refusing to make the home directory a project; run this in the project's own directory")
	}
	p, err := config.Create(root, name, dir, checkout.Origin(dir))
	if err != nil {
		return core.Project{}, err
	}
	checkout.Trust(dir, os.Stderr)
	fmt.Printf("created %s\n", p.File)
	return p, nil
}
