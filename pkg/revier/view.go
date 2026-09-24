package revier

// ProjectView is what the core produces and both renderers - the TUI and
// --json - consume. It is the only type they share, so a field added here
// serves both surfaces at once.
type ProjectView struct {
	Project Project `json:"project"`
	Running bool    `json:"running"`
	// PathExists reports whether the project directory is on this machine. A
	// project file outlives the checkout it names - a machine that never had
	// it, a directory that was deleted - and a target launched into a path
	// that is not there fails in the tool rather than in revier.
	PathExists bool `json:"path_exists"`
	// Unreachable says why a remote project's host gave no answer: the
	// text of the failure, empty when it answered. Its agents and its
	// checkout are then unknown, and the view carries none of either.
	Unreachable string `json:"unreachable,omitempty"`
	// Invalid says why nothing in this project can run: its file did not
	// parse, or a rule about the project as a whole refused it. The project
	// is listed anyway, because a file that vanishes from the surface takes
	// with it the one place the reason could be read.
	Invalid string       `json:"invalid,omitempty"`
	Home    TargetRef    `json:"home,omitzero"`
	Targets []TargetView `json:"targets"`
	Agents  []AgentView  `json:"agents,omitempty"`
}

// Held reports whether anything on this machine holds the project: its home
// target, any other target, or a terminal attached to it. Running alone is the
// home target, which is what run-or-raise and a restore act on; a surface
// draws Held, so a project with an attached terminal and no home reads as
// open. Every agent a surface shows sits in a panel of an instance this
// counts, so a project that reads as closed shows no agents (D104).
func (v ProjectView) Held() bool {
	if v.Running {
		return true
	}
	for _, t := range v.Targets {
		if !t.Ref.IsZero() {
			return true
		}
	}
	return false
}

// Attention reports whether any agent in the project is waiting for the human.
// The TUI sorts on it.
func (v ProjectView) Attention() bool {
	for _, a := range v.Agents {
		if a.State.Status == StatusAttention {
			return true
		}
	}
	return false
}

// TargetView is one target and the instance backing it, if any. Host names
// which realization won. Attached is true for an instance bound at runtime
// rather than declared in config, which has no Name and no Key.
type TargetView struct {
	Name      TargetName `json:"name,omitempty"`
	Key       string     `json:"key,omitempty"`
	Host      string     `json:"host,omitempty"`
	Ref       TargetRef  `json:"ref,omitzero"`
	Attached  bool       `json:"attached,omitempty"`
	Available bool       `json:"available"` // a host that can realize it is configured
	// Reason says why Available is false. A target no host here can realize
	// and a target its own config refused are both unavailable, and only the
	// reason tells the user which of the two to go and fix.
	Reason string `json:"reason,omitempty"`
	// Unknown says why the host that realizes the target could not list its
	// instances this survey: the target may be running or not, and a press
	// on it is refused with this reason until the host answers again.
	Unknown string `json:"unknown,omitempty"`
}

type AgentView struct {
	Panel PanelID `json:"panel"`
	// Ref is the instance holding the panel. A panel id is unique only within
	// the process that numbers it, so the two together name one agent.
	Ref   TargetRef  `json:"ref,omitzero"`
	State AgentState `json:"state"`
	// Conversation is set only in the answer to Remote.Conversations: what a
	// save records for an agent whose process is on another machine.
	Conversation *Conversation `json:"conversation,omitempty"`
}
