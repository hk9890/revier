package revier

import (
	"context"
	"errors"
)

// Remote is a revier on another machine (decisions.md D40). It is asked what
// it knows, and nothing else: it lists no instances, opens nothing and focuses
// nothing here. What runs on the remote machine is that revier's business,
// surveyed by its own hosts and probes, and this side reads the view it
// produces. The terminals that show what runs there are panels of this
// machine's runtime, and are driven as such (D84).
type Remote interface {
	// Name is the host as the project file names it.
	Name() string

	// Version is the version the remote revier reports for itself. It
	// surveys nothing, so it is the cheap way to learn that the machine
	// answers and runs a revier. A machine that answers and runs none is
	// ErrNoRevier.
	Version(ctx context.Context) (string, error)

	// Survey reports the named projects as the remote revier sees them, one
	// call for all of them. A name the remote does not know is an error, so
	// a missing project file there is reported, not shown as a project with
	// nothing in it.
	Survey(ctx context.Context, names []ProjectName) ([]ProjectView, error)

	// Conversations is Survey with each agent's Conversation filled in, for a
	// save. It is a call of its own because naming the conversations costs
	// the remote a process a survey does not pay for.
	Conversations(ctx context.Context, names []ProjectName) ([]ProjectView, error)

	// Events is what the remote revier recorded in the last days, about every
	// project it has, as `revier events --local` prints it there
	// (decisions.md D112). The answer is that machine's alone: it asks no
	// host of its own.
	Events(ctx context.Context, days int) ([]Event, error)

	// RunCommand is the argv that runs `revier run <action> -p <project>`
	// on the remote, for the caller to run here with the terminal: an
	// action takes the terminal it is given, so it is not run and read back
	// but handed the one the user is at. The action is the remote's to
	// know; its configuration there defines it.
	RunCommand(project ProjectName, action string) []string

	// PanelCommand is the argv a panel of this machine's runtime runs to show
	// the project's agent or shell: `revier <kind> exec -p <project>` on the
	// remote, kind being PanelAgent or PanelShell (decisions.md D84). A
	// non-empty agent is the named agent the panel runs, `--agent <agent>`
	// there: the remote's configuration declares it, and refuses a name it
	// does not (D146). Arguments appended to the argv reach that command as
	// they are, which is how a restore passes the conversation to resume and
	// the directory to start in.
	PanelCommand(project ProjectName, kind PanelKind, agent string) []string
}

// ErrNoRevier is a machine that answers and has no revier to run: what a
// Remote's call wraps where the remote shell could not find it.
var ErrNoRevier = errors.New("no revier there")
