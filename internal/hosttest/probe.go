package hosttest

import (
	"context"
	"strings"
	"sync"

	"github.com/hk9890/revier/pkg/revier"
)

// TitleProbe reads an agent off its panel's title, the way the Claude probe
// reads a glyph: the state lives in the panel, so a test changes it by
// retitling. It claims a panel whose command is the harness, or whose title
// starts with it. After the harness, the first word of the title is the state
// - idle, busy or ask, and any other word is unknown - and the rest is the
// activity.
type TitleProbe struct{ Harness string }

func (p TitleProbe) Name() string { return p.Harness }

func (p TitleProbe) Match(panel revier.Panel) bool {
	return len(panel.Command) > 0 && panel.Command[0] == p.Harness || strings.HasPrefix(panel.Title, p.Harness+" ")
}

func (p TitleProbe) Inspect(_ context.Context, panel revier.Panel) (revier.AgentState, error) {
	state, activity, _ := strings.Cut(strings.TrimPrefix(panel.Title, p.Harness+" "), " ")
	status := map[string]revier.Status{
		"idle": revier.StatusIdle, "busy": revier.StatusRunning, "ask": revier.StatusAttention,
	}[state]
	return revier.AgentState{Harness: p.Harness, Status: status, Activity: activity}, nil
}

// FakeProbe is an AgentProbe that reports a fixed state for every panel whose
// title carries a marker. It exists so core tests can assert on the survey's
// agent half without a real harness.
type FakeProbe struct {
	Harness string
	Marker  string // a panel matches when its title contains this
	State   revier.AgentState
	Err     error

	mu sync.Mutex
	// reads counts Inspect calls, so a test can assert that a survey reads a
	// panel once whatever the project count. A survey dispatches its probes
	// from more than one goroutine, so the count is kept under the mutex and
	// read through Reads.
	reads int
}

// Reads is how many panels Inspect was asked about.
func (p *FakeProbe) Reads() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reads
}

func (p *FakeProbe) Name() string { return p.Harness }

func (p *FakeProbe) Match(panel revier.Panel) bool {
	return p.Marker == "" || strings.Contains(panel.Title, p.Marker)
}

func (p *FakeProbe) Inspect(context.Context, revier.Panel) (revier.AgentState, error) {
	p.mu.Lock()
	p.reads++
	p.mu.Unlock()
	if p.Err != nil {
		return revier.AgentState{}, p.Err
	}
	return p.State, nil
}

// FakeResumableProbe is a FakeProbe that can also name the conversation a panel
// holds, so a core test can exercise the resume half without Claude Code. The
// id is the panel's Vars["session"] and the directory its Vars["dir"], a
// stand-in for the listing the real probe matches panels against.
type FakeResumableProbe struct {
	*FakeProbe
	// Flag is the argument name folded into a resumed command, standing in
	// for the harness's own spelling of --resume.
	Flag string
	// SessionErr makes Sessions fail, for the path where a panel restores
	// empty rather than failing the save.
	SessionErr error
	// Calls counts Sessions calls, so a test can assert a save asks once
	// whatever the number of agents.
	Calls int
}

// NewResumableProbe returns a probe that claims every panel of the harness.
func NewResumableProbe(harness, marker string) *FakeResumableProbe {
	return &FakeResumableProbe{
		FakeProbe: &FakeProbe{Harness: harness, Marker: marker},
		Flag:      "--resume",
	}
}

func (p *FakeResumableProbe) Sessions(_ context.Context, panels []revier.Panel) ([]revier.Conversation, error) {
	p.Calls++
	if p.SessionErr != nil {
		return nil, p.SessionErr
	}
	out := make([]revier.Conversation, len(panels))
	for i, panel := range panels {
		out[i] = revier.Conversation{ID: revier.SessionID(panel.Vars["session"]), Dir: panel.Vars["dir"]}
	}
	return out, nil
}

func (p *FakeResumableProbe) ResumeCommand(spec revier.PanelSpec, id revier.SessionID) []string {
	cmd := append([]string{}, spec.Command...)
	return append(cmd, p.Flag, string(id))
}

// FakeDetailedProbe is a FakeProbe that can also say what the agent in a panel
// said last, so a test can exercise the pane's message without a transcript.
// The answer is Said's entry for the panel's id, and the zero detail for a
// panel it has none for.
type FakeDetailedProbe struct {
	*FakeProbe
	Said map[revier.PanelID]revier.AgentDetail
	// DetailErr makes Detail fail, for the path where the pane shows nothing.
	DetailErr error

	calls int
}

// NewDetailedProbe returns a probe that claims every panel of the harness and
// has said nothing yet.
func NewDetailedProbe(harness, marker string) *FakeDetailedProbe {
	return &FakeDetailedProbe{
		FakeProbe: &FakeProbe{Harness: harness, Marker: marker},
		Said:      map[revier.PanelID]revier.AgentDetail{},
	}
}

// DetailCalls is how many panels Detail was asked about.
func (p *FakeDetailedProbe) DetailCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *FakeDetailedProbe) Detail(_ context.Context, panel revier.Panel) (revier.AgentDetail, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.DetailErr != nil {
		return revier.AgentDetail{}, p.DetailErr
	}
	return p.Said[panel.ID], nil
}
