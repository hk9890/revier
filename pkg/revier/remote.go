package revier

import "context"

// Remote is a revier on another machine (decisions.md D40). It is asked what
// it knows and told what to type, and nothing else: it lists no instances,
// opens nothing and focuses nothing here. What runs on the remote machine is
// that revier's business, surveyed by its own hosts and probes, and this side
// reads the view it produces.
type Remote interface {
	// Name is the host as the project file names it.
	Name() string

	// Survey reports the named projects as the remote revier sees them, one
	// call for all of them. A name the remote does not know is an error, so
	// a missing project file there is reported, not shown as a project with
	// nothing in it.
	Survey(ctx context.Context, names []ProjectName) ([]ProjectView, error)

	// Prompt types one line into the agent an address names and submits it,
	// with the remote's own refusals: `revier agent prompt` there.
	Prompt(ctx context.Context, address, text string) error

	// Wait blocks until the agent an address names reaches one of the
	// statuses until names, and returns the status it ended on. It ends
	// early with ctx's error.
	Wait(ctx context.Context, address, until string) (Status, error)
}
