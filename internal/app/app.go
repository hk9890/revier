// Package app is the use cases a driving surface asks for that are a sequence
// across more than one store: the project files, a checkout, the state, the
// saved sessions and the events (decisions.md D121). The command line and the
// TUI call it and keep only what is theirs: taking the input and showing the
// result. A decision about a target or a host is not here; it is the core's.
package app

import (
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/hk9890/revier/internal/checkout"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// CreateProject writes the project file for dir under name, or under the
// directory's own name when name is empty. What config.CanCreate refuses is
// refused here.
//
// A directory that is there records its own origin, so the project can be
// cloned on the next machine, and gitURL is not used. Its mise configuration
// is trusted once the file is written, as an open trusts the checkout it
// clones: a file that could not be written leaves nothing trusted. What the
// trust has to say goes to warn. A directory that is not there records
// gitURL, the repository the caller clones into it, or nothing.
//
// The state record has the create, refused or not (decisions.md D151).
func CreateProject(cfgRoot string, ledger core.Ledger, projects []core.Project, name revier.ProjectName, dir, gitURL string, warn io.Writer) (p core.Project, err error) {
	if name == "" {
		name = config.NameFor(dir)
	}
	defer func() { ledger.Note(record.Line{Kind: record.KindProjectCreated, Project: name, Dir: dir}.Failed(err)) }()
	if err := config.CanCreate(projects, name, dir); err != nil {
		return core.Project{}, err
	}
	info, err := os.Stat(dir)
	there := err == nil && info.IsDir()
	if there {
		gitURL = checkout.Origin(dir)
	}
	p, err = config.Create(cfgRoot, name, dir, gitURL)
	if err != nil {
		return core.Project{}, err
	}
	if there {
		checkout.Trust(dir, warn)
	}
	return p, nil
}

// LinkProject writes a link under name to the project a host reports as on,
// or under the project's own name when name is empty. A name whose file is
// there is refused, so no project is overwritten, and so is a host that is
// this machine, since revier leaves such a link out (decisions.md D131).
// The state record has the link, refused or not.
func LinkProject(cfgRoot string, ledger core.Ledger, name revier.ProjectName, host string, on revier.Project) (p core.Project, err error) {
	if name == "" {
		name = on.Name
	}
	defer func() {
		ledger.Note(record.Line{Kind: record.KindProjectLinked, Project: name, Machine: host}.Failed(err))
	}()
	if err := config.ThisMachine().RefuseLink(host); err != nil {
		return core.Project{}, err
	}
	return config.CreateLink(cfgRoot, name, host, on)
}

// RenameProject moves a project to a new name in the three stores that hold
// it by name: the project file, the state, and the saved sessions
// (decisions.md D80). The file is first, and a file that cannot be moved
// changes nothing. The saved sessions are last and cannot fail the rename:
// one that keeps the old name no longer opens the project, which the log
// says, and the project itself is renamed. The state record has the rename,
// failed or not.
func RenameProject(cfgRoot, stateRoot string, ledger core.Ledger, from, to revier.ProjectName, inh config.Inherited) (core.Project, error) {
	p, err := config.Rename(cfgRoot, from, to, inh)
	ledger.Note(record.Line{Kind: record.KindProjectRenamed, Project: to, From: string(from)}.Failed(err))
	if err != nil {
		return core.Project{}, err
	}
	ledger.Update(func(st *state.State) bool {
		st.Rename(from, to)
		return true
	})
	if err := session.Rename(stateRoot, from, to); err != nil {
		slog.Warn("rename: the saved sessions keep the old name", "from", from, "to", to, "err", err)
	}
	return p, nil
}

// DeleteProject removes the project's file. The state record has the delete,
// failed or not.
func DeleteProject(ledger core.Ledger, p core.Project) error {
	err := os.Remove(p.File)
	ledger.Note(record.Line{Kind: record.KindProjectDeleted, Project: p.Name, Dir: p.File}.Failed(err))
	if err == nil {
		slog.Info("project deleted", "project", p.Name, "path", p.File)
	}
	return err
}

// Action is one run of an action a surface is about to make: the command, and
// what to tell revier once it has ended.
type Action struct {
	Argv []string
	Dir  string

	project revier.ProjectName
	machine string
	here    bool
	name    string
	start   time.Time
	ledger  core.Ledger
}

// PlanAction is the command an action runs for a project, with its launch
// already written: an action may open anything, and the window that appears
// next is the project's (claim-on-appear). The launch is stamped with the
// moment of the plan, so its claim window runs from the action's start and
// not from its exit, which for an editor is hours later. The caller runs the
// command and then calls Done.
//
// A link's action runs on its host and opens no window here, so it writes no
// launch.
func PlanAction(c *core.Core, p core.Project, name string, run []string) (Action, error) {
	argv, dir, err := c.ActionCommand(p, name, run)
	if err != nil {
		return Action{}, err
	}
	a := Action{Argv: argv, Dir: dir, project: p.Name, here: p.Remote == nil, name: name, start: time.Now(), ledger: c.Ledger}
	if p.Remote != nil {
		a.machine = p.Remote.Host
	}
	if a.here {
		a.ledger.Update(func(st *state.State) bool {
			st.Launched(a.project, "", a.start)
			return true
		})
	}
	return a, nil
}

// Done takes how the command ended. An action that succeeded is one event,
// stamped with its start as the launch is: an action runs as long as it runs,
// and it was asked for when it began. A link's action is the event of the
// revier that ran it on its host, which `revier events` prints with that
// host, so it is no second one here. The state record has every action,
// failed or not, a link's too.
func (a Action) Done(err error) {
	logging.Op("action", a.start, err, "project", a.project, "action", a.name, "argv", a.Argv)
	if err == nil && a.here {
		a.ledger.Record(revier.Event{Time: a.start, Kind: revier.EventAction, Project: a.project, Action: a.name})
	}
	a.ledger.Note(record.Line{Kind: record.KindAction, Project: a.project, Machine: a.machine, Action: a.name}.Failed(err))
}
