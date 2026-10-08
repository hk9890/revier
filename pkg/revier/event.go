package revier

import "time"

// The kinds of Event. They are a contract with whatever reads `revier
// events`: a kind is added, never renamed (decisions.md D112).
const (
	// EventGo is one press of a target that ran or raised it. A step of a
	// restore is not one.
	EventGo = "go"
	// EventGoAgent is one press of an agent that brought it to the front, in
	// the surface or by `revier agent focus`.
	EventGoAgent = "go agent"
	// EventAgentNew is an agent tab added to an open workspace.
	EventAgentNew = "agent new"
	// EventShellNew is a shell tab added to an open workspace.
	EventShellNew = "shell new"
	// EventAction is a configured action that ran and exited 0.
	EventAction = "action"
	// EventAgentSession is a surface seeing an agent hold a conversation:
	// once when it first sees it, and once on each later day it still does.
	EventAgentSession = "agent session"
)

// Event is one thing revier did in a project, or one conversation it saw an
// agent hold there: a line of the event file, and of `revier events`. It names
// what happened and where, never what was said (decisions.md D112).
type Event struct {
	// Time is when it was asked for: the press of a go, the start of an
	// action, and for an EventAgentSession the survey that saw it.
	Time time.Time `json:"time"`
	Kind string    `json:"event"`
	// Host is the machine whose revier recorded the event, as a project file
	// here names it. It is empty for this machine, and set only by the read
	// that asked the host.
	Host    string      `json:"host,omitempty"`
	Project ProjectName `json:"project"`
	// Target is where an EventGo landed - the tab it made current, else the
	// target - and the workspace a new tab opened in.
	Target TargetName `json:"target,omitempty"`
	// Launched reports that an EventGo started the target, not raised it.
	Launched bool `json:"launched,omitempty"`
	// Action is the name of the action an EventAction ran.
	Action string `json:"action,omitempty"`
	// Agent is the harness of the agent an EventGoAgent or an
	// EventAgentSession is of, and Session the conversation it holds, which
	// a tab resumed with --resume names too.
	Agent   string    `json:"agent,omitempty"`
	Session SessionID `json:"session,omitempty"`
	// Dir is the directory an agent works in or a tab started in. For a link
	// it is a path on the link's host, as Session is a conversation there.
	Dir string `json:"dir,omitempty"`
}
