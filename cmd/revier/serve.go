package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"syscall"

	"github.com/hk9890/revier/internal/adapter/proc"
	"github.com/hk9890/revier/internal/checkout"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// cmdAgentExec becomes the project's agent, for a terminal on another machine:
// it is what the agent panel of a link runs here over ssh (decisions.md D84).
func cmdAgentExec(args []string) error {
	fs := flag.NewFlagSet("agent exec", flag.ContinueOnError)
	project := projectFlag(fs)
	tag := fs.String("tag", "", "the name the terminal that shows the agent knows it by")
	resume := fs.String("resume", "", "the conversation to start the agent on")
	dir := fs.String("dir", "", "the directory the agent starts in")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || *project == "" {
		return errors.New("usage: revier agent exec -p <project> [--tag <tag>] [--resume <id>] [--dir <path>]")
	}
	return serve(*project, *tag, func(c *core.Core, p core.Project) (core.Served, error) {
		// A checkout that is missing here is cloned by the agent's panel
		// alone: the shell's starts beside it, and two clones into one
		// directory fail each other.
		if _, err := checkout.Ensure(p.Project, os.Stderr); err != nil {
			return core.Served{}, err
		}
		s, err := c.ServeAgent(p, core.Resume{Session: revier.SessionID(*resume), Dir: *dir})
		if err == nil && *resume != "" && s.Outcome != core.AgentResumed {
			fmt.Fprintf(os.Stderr, "revier: warning: %s was not resumed (%s); the agent starts empty\n", *resume, s.Outcome)
		}
		return s, err
	})
}

// cmdShellExec becomes the project's shell, as cmdAgentExec becomes its agent.
func cmdShellExec(args []string) error {
	fs := flag.NewFlagSet("shell exec", flag.ContinueOnError)
	project := projectFlag(fs)
	tag := fs.String("tag", "", "the name the terminal that shows the shell knows it by")
	dir := fs.String("dir", "", "the directory the shell starts in")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || *project == "" {
		return errors.New("usage: revier shell exec -p <project> [--tag <tag>] [--dir <path>]")
	}
	return serve(*project, *tag, func(c *core.Core, p core.Project) (core.Served, error) {
		return c.ServeShell(p, *dir)
	})
}

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
	warnProblems(projects)
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
