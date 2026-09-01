// Package hosttest provides the fake Host that layer L2 runs against.
//
// It is the reason most of revier can be tested without a terminal, a
// compositor, or a screen: the core's behaviour - resolution, matching,
// run-or-raise, toggle-back - depends only on what a host reports, and a fake
// reports whatever a test needs. See docs/TESTING.md for the layer model.
package hosttest

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/hk9890/revier/pkg/revier"
)

// Fake is an in-memory Host. The zero value is unusable; construct it with New.
type Fake struct {
	mu sync.Mutex

	name      string
	instances []revier.Instance
	focused   revier.TargetRef
	nextID    int

	// ProbeErr makes Probe fail, so a test can exercise selection falling
	// through to the next adapter.
	ProbeErr error
	// InstancesErr makes Instances fail, for the survey error path.
	InstancesErr error
	// OpenErr makes Open fail, for the run half of run-or-raise.
	OpenErr error

	// Opened records every realization passed to Open, in order. A test
	// asserts on it to prove the core rendered templates before the host saw
	// them, and that run-or-raise ran rather than raised.
	Opened []revier.Realization
	// Focuses records every ref passed to Focus, in order.
	Focuses []revier.TargetRef

	caps revier.Capabilities
}

// New returns a fake host with the given name.
func New(name string) *Fake {
	return &Fake{name: name, caps: revier.Capabilities{Layout: true}}
}

// NewRuntime returns a fake that satisfies revier.Runtime.
func NewRuntime(name string) *FakeRuntime { return &FakeRuntime{Fake: New(name)} }

// FakeRuntime is a Fake that also reports capabilities.
type FakeRuntime struct{ *Fake }

func (f *FakeRuntime) Capabilities() revier.Capabilities { return f.caps }

// SetCapabilities changes what the runtime reports, for tests that assert on
// the no-layout path.
func (f *FakeRuntime) SetCapabilities(c revier.Capabilities) { f.caps = c }

// Add registers a live instance and returns its ref.
func (f *Fake) Add(title, class string, panels ...revier.Panel) revier.TargetRef {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	ref := revier.TargetRef{Host: f.name, ID: strconv.Itoa(f.nextID), Title: title}
	f.instances = append(f.instances, revier.Instance{
		Ref: ref, Title: title, Class: class, PID: 1000 + f.nextID, Panels: panels,
	})
	return ref
}

// Remove drops an instance, for tests that assert on a target disappearing.
func (f *Fake) Remove(ref revier.TargetRef) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, in := range f.instances {
		if in.Ref.ID == ref.ID {
			f.instances = append(f.instances[:i], f.instances[i+1:]...)
			return
		}
	}
}

// SetFocus makes ref the focused instance.
func (f *Fake) SetFocus(ref revier.TargetRef) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.focused = ref
}

func (f *Fake) Name() string { return f.name }

func (f *Fake) Probe(context.Context) error { return f.ProbeErr }

func (f *Fake) Instances(context.Context) ([]revier.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.InstancesErr != nil {
		return nil, f.InstancesErr
	}
	out := make([]revier.Instance, len(f.instances))
	copy(out, f.instances)
	return out, nil
}

// Open records the realization and materialises an instance whose title is the
// realization's own title pattern, so a follow-up match finds what was opened.
func (f *Fake) Open(_ context.Context, r revier.Realization) (revier.TargetRef, error) {
	f.mu.Lock()
	f.Opened = append(f.Opened, r)
	f.mu.Unlock()
	if f.OpenErr != nil {
		return revier.TargetRef{}, f.OpenErr
	}
	if len(r.Launch) == 0 {
		return revier.TargetRef{}, fmt.Errorf("%s: realization has no launch argv", f.name)
	}
	// Honour the invariant every real host must honour: what Open creates,
	// this realization's Match finds. Name wins when set, as it does for tmux;
	// otherwise the match patterns describe the instance.
	title := literal(r.Match.Title)
	if r.Name != "" {
		title = r.Name
	}
	return f.Add(title, literal(r.Match.Class)), nil
}

func (f *Fake) Focus(_ context.Context, ref revier.TargetRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Focuses = append(f.Focuses, ref)
	f.focused = ref
	return nil
}

func (f *Fake) Focused(context.Context) (revier.TargetRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.focused, nil
}

// literal strips the anchors a match pattern carries so the fake can turn a
// pattern back into a plausible title. Tests use plain anchored literals; a
// pattern with real metacharacters is a test that should assert on Opened
// instead.
func literal(pattern string) string {
	s := pattern
	if len(s) > 0 && s[0] == '^' {
		s = s[1:]
	}
	if len(s) > 0 && s[len(s)-1] == '$' {
		s = s[:len(s)-1]
	}
	return s
}

// FakeProbe is an AgentProbe that reports a fixed state for every panel whose
// title carries a marker. It exists so core tests can assert on the survey's
// agent half without a real harness.
type FakeProbe struct {
	Harness string
	Marker  string // a panel matches when its title contains this
	State   revier.AgentState
	Err     error
}

func (p *FakeProbe) Name() string { return p.Harness }

func (p *FakeProbe) Match(panel revier.Panel) bool {
	return p.Marker == "" || contains(panel.Title, p.Marker)
}

func (p *FakeProbe) Inspect(context.Context, revier.Panel) (revier.AgentState, error) {
	if p.Err != nil {
		return revier.AgentState{}, p.Err
	}
	return p.State, nil
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
