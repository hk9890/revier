package revier

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type PanelID string

// PanelSpec declares one pane of a Home target's layout.
type PanelSpec struct {
	Kind    PanelKind `toml:"kind" json:"kind"`
	Title   string    `toml:"title" json:"title,omitempty"`
	Command []string  `toml:"command" json:"command,omitempty"`

	// Dir is where this panel starts. No project file sets it: the core fills
	// it with the realization's Dir when it renders the project, so a host
	// starts a panel in its Dir and decides nothing. A restore sets another,
	// for an agent that worked in a worktree of the project rather than in the
	// project itself (decisions.md D62), and so does `revier agent new`.
	Dir string `toml:"-" json:"dir,omitempty"`
}

type PanelKind string

const (
	PanelAgent PanelKind = "agent"
	PanelShell PanelKind = "shell"
	PanelTool  PanelKind = "tool"
)

// Panel is one live pane inside a runtime instance. A probe reads this and
// nothing else.
type Panel struct {
	ID      PanelID           `json:"id"`
	Kind    PanelKind         `json:"kind"`
	Title   string            `json:"title"` // live OSC title, as the process last set it
	Vars    map[string]string `json:"vars,omitempty"`
	PID     int               `json:"pid,omitempty"`
	Command []string          `json:"command,omitempty"`
}

// Runs reports whether the panel's foreground command is the program called
// name: its argv[0], or the script an interpreter was started on - `node
// .../bin/claude` and `python3 -m aider` are how an npm or a pip install runs
// one. Any other argument is data: `nvim claude` edits a file, and a probe
// that claimed it would have `revier agent prompt` type into the editor.
func (p Panel) Runs(name string) bool {
	if len(p.Command) == 0 {
		return false
	}
	program := baseName(p.Command[0])
	if program == name {
		return true
	}
	if !interpreter(program) {
		return false
	}
	for _, arg := range p.Command[1:] {
		if !strings.HasPrefix(arg, "-") {
			return baseName(arg) == name
		}
	}
	return false
}

func interpreter(program string) bool {
	switch program {
	case "node", "bun", "deno":
		return true
	}
	return strings.HasPrefix(program, "python")
}

func baseName(arg string) string { return arg[strings.LastIndex(arg, "/")+1:] }

// AgentProbe derives agent state from a panel.
//
// Inspect receives the whole Panel rather than a narrow signal, because which
// signal is trustworthy differs per harness and is not obvious. Claude Code is
// the worked example: its title glyph is a level, describing the state right
// now, while the user variable its hooks set records edges and reports "busy"
// indefinitely after a turn interrupted with Esc. The probe reads the glyph and
// treats the variable as advisory.
type AgentProbe interface {
	Name() string

	// Match reports whether this probe understands the panel. The first
	// matching probe wins, in configured order.
	Match(p Panel) bool

	Inspect(ctx context.Context, p Panel) (AgentState, error)
}

type AgentState struct {
	Harness  string    `json:"harness"`
	Status   Status    `json:"status"`
	Activity string    `json:"activity,omitempty"`
	Since    time.Time `json:"since,omitzero"`
}

// Status is what an agent is doing. StatusAttention is the state the product
// exists to surface: it drives the TUI sort order.
type Status uint8

const (
	StatusUnknown Status = iota
	StatusIdle
	StatusRunning
	StatusAttention
)

var statusNames = map[Status]string{
	StatusUnknown:   "unknown",
	StatusIdle:      "idle",
	StatusRunning:   "running",
	StatusAttention: "attention",
}

func (s Status) String() string {
	if n, ok := statusNames[s]; ok {
		return n
	}
	return "unknown"
}

// MarshalJSON writes the status as its name. External probes exchange these
// names, so the wire form is part of the extension contract.
func (s Status) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

func (s *Status) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return err
	}
	parsed, err := ParseStatus(name)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// ParseStatus reads a status from its name, the word String writes.
func ParseStatus(name string) (Status, error) {
	for v, n := range statusNames {
		if n == name {
			return v, nil
		}
	}
	return StatusUnknown, fmt.Errorf("unknown agent status %q", name)
}

// String makes a PanelID usable where a host expects a plain target argument.
func (p PanelID) String() string { return string(p) }

// SessionID is a harness's own name for one conversation, opaque to revier.
// Only the probe that produced it knows how to spell it back into an argv.
type SessionID string

// Conversation is what a probe can say about the conversation a panel holds:
// its id, and the directory the agent works in. The directory is the agent's
// and not the panel's: an agent started in a project can work in one of its
// worktrees, and resumed anywhere else it carries on in the wrong checkout.
// Either is empty when the harness does not say.
type Conversation struct {
	ID  SessionID
	Dir string
}

// Resumable is an optional capability of an AgentProbe, detected by type
// assertion. A probe that implements it can name the conversation a panel
// holds, so a workspace reopened after a reboot starts its agent where the
// agent was rather than empty. A probe that does not restores an empty agent,
// which is the whole of the degradation.
//
// It exists because a snapshot records names and nothing else: the argv is
// derived again at restore from the project file as it reads then, so an
// edited project file wins over a stale recording. ResumeCommand is handed
// that current spec and folds its own resume flag into it, which is why the
// flag's spelling never reaches the core.
type Resumable interface {
	// Sessions names the conversation each panel holds, in the panels'
	// order, with a zero Conversation for a panel that holds none. It is
	// asked once for every panel of a save rather than once per panel,
	// because the answer may cost a process - `claude agents --json` - and a
	// save of twenty agents must cost that once, the rule Host.Instances
	// follows.
	Sessions(ctx context.Context, panels []Panel) ([]Conversation, error)

	// ResumeCommand returns the argv that starts the harness on that
	// conversation, built from the panel as it is configured now. The
	// configured arguments are kept: a project that runs its agent with a
	// model flag keeps the flag across a restore.
	ResumeCommand(spec PanelSpec, id SessionID) []string
}
