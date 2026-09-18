package hosttest

import (
	"context"
	"sync"

	"github.com/hk9890/revier/pkg/revier"
)

// FakeRemote is an in-memory revier.Remote: the revier on another machine,
// answering with whatever views a test gives it.
type FakeRemote struct {
	mu   sync.Mutex
	name string

	// Views is what Survey answers with, whatever names it was asked for.
	Views []revier.ProjectView
	// Err makes every call fail, for the unreachable host path.
	Err error

	// Asked records the names of every Survey call, in order, so a test can
	// assert that a refresh costs one call per host whatever the project
	// count.
	Asked [][]revier.ProjectName
	// RunArgv is what RunCommand answers with; Runs records every ask.
	RunArgv []string
	Runs    []Run
	// Named is what Conversations answers with.
	Named []revier.ProjectView
}

// Run is one action asked for on one project.
type Run struct {
	Project revier.ProjectName
	Action  string
}

func (f *FakeRemote) RunCommand(project revier.ProjectName, action string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Runs = append(f.Runs, Run{Project: project, Action: action})
	return append([]string(nil), f.RunArgv...)
}

// NewRemote returns a fake remote for the host.
func NewRemote(name string, views ...revier.ProjectView) *FakeRemote {
	return &FakeRemote{name: name, Views: views}
}

func (f *FakeRemote) Name() string { return f.name }

func (f *FakeRemote) Survey(_ context.Context, names []revier.ProjectName) ([]revier.ProjectView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Asked = append(f.Asked, append([]revier.ProjectName(nil), names...))
	if f.Err != nil {
		return nil, f.Err
	}
	return answer(f.Views), nil
}

func (f *FakeRemote) Conversations(context.Context, []revier.ProjectName) ([]revier.ProjectView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return answer(f.Named), nil
}

// answer is a copy of the views down to their agents, as an answer decoded
// from JSON would be: the core renames a link's agents in place, and the
// next call must answer as the host does, not as the core left it.
func answer(views []revier.ProjectView) []revier.ProjectView {
	out := append([]revier.ProjectView(nil), views...)
	for i := range out {
		out[i].Agents = append([]revier.AgentView(nil), out[i].Agents...)
	}
	return out
}
