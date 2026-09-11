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
	// WaitStatus is what Wait answers with.
	WaitStatus revier.Status

	// Asked records the names of every Survey call, in order, so a test can
	// assert that a refresh costs one call per host whatever the project
	// count.
	Asked [][]revier.ProjectName
	// Prompts records every prompt, in order.
	Prompts []Prompt
	// Waits records every wait, in order.
	Waits []Wait
	// RunArgv is what RunCommand answers with; Runs records every ask.
	RunArgv []string
	Runs    []Run
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

// Prompt is one text sent to one address.
type Prompt struct {
	Address, Text string
}

// Wait is one wait on one address.
type Wait struct {
	Address, Until string
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
	return append([]revier.ProjectView(nil), f.Views...), nil
}

func (f *FakeRemote) Prompt(_ context.Context, address, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Prompts = append(f.Prompts, Prompt{Address: address, Text: text})
	return f.Err
}

func (f *FakeRemote) Wait(_ context.Context, address, until string) (revier.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Waits = append(f.Waits, Wait{Address: address, Until: until})
	if f.Err != nil {
		return revier.StatusUnknown, f.Err
	}
	return f.WaitStatus, nil
}
