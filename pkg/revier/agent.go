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
	for v, n := range statusNames {
		if n == name {
			*s = v
			return nil
		}
	}
	return fmt.Errorf("unknown agent status %q", name)
}

// String makes a PanelID usable where a host expects a plain target argument.
func (p PanelID) String() string { return string(p) }
