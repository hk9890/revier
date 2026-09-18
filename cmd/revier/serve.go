package main

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"syscall"

	"github.com/hk9890/revier/internal/adapter/proc"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
)

// serve replaces this process with what a project serves. The process keeps
// its pid, and everything it starts inherits the two variables, which is how
// the served-processes host finds the agent again under its tag. It asks no
// host anything: the project file alone says what runs.
func serve(project, tag string, what func(*core.Core, core.Project) (core.Served, error)) error {
	cfgRoot, err := config.Root()
	if err != nil {
		return err
	}
	cfg, projects, err := config.Load(cfgRoot)
	if err != nil {
		return err
	}
	warnProblems(cfg, projects)
	i := slices.IndexFunc(projects, func(p core.Project) bool { return string(p.Name) == project })
	if i < 0 {
		return fmt.Errorf("no project named %q", project)
	}
	// A project its file refused as a whole serves nothing, and says why: the
	// panel on the other machine is the one place its user reads the reason.
	if p := projects[i]; p.Invalid != nil {
		return p.Invalid
	}
	s, err := what(newCore(cfg, nil, nil, nil), projects[i])
	if err != nil {
		return err
	}
	if s.Dir != "" {
		if err := os.Chdir(s.Dir); err != nil {
			fmt.Fprintf(os.Stderr, "revier: warning: %v; starting in %s\n", err, mustGetwd())
		}
	}
	path, err := exec.LookPath(s.Argv[0])
	if err != nil {
		return err
	}
	env := os.Environ()
	if tag != "" {
		env = append(env, proc.TagVar+"="+tag, proc.WorkspaceVar+"="+s.Workspace)
	}
	return syscall.Exec(path, s.Argv, env)
}

func mustGetwd() string {
	dir, _ := os.Getwd()
	return dir
}
