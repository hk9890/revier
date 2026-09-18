package revier

import "context"

// Remote is a revier on another machine (decisions.md D40). It is asked what
// it knows, and nothing else: it lists no instances, opens nothing and focuses
// nothing here. What runs on the remote machine is that revier's business,
// surveyed by its own hosts and probes, and this side reads the view it
// produces. The terminals that show what runs there are panels of this
// machine's runtime, and are driven as such (decisions.md D84).
type Remote interface {
	// Name is the host as the project file names it.
	Name() string

	// Survey reports the named projects as the remote revier sees them, one
	// call for all of them. A name the remote does not know is an error, so
	// a missing project file there is reported, not shown as a project with
	// nothing in it.
	Survey(ctx context.Context, names []ProjectName) ([]ProjectView, error)

	// Conversations is Survey with each agent's Conversation filled in, for a
	// save. It is a call of its own because naming the conversations costs
	// the remote a process a survey does not pay for.
	Conversations(ctx context.Context, names []ProjectName) ([]ProjectView, error)

	// RunCommand is the argv that runs `revier run <action> -p <project>`
	// on the remote, for the caller to run here with the terminal: an
	// action takes the terminal it is given, so it is not run and read back
	// but handed the one the user is at. The action is the remote's to
	// know; its configuration there defines it.
	RunCommand(project ProjectName, action string) []string
}
