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
	Unreachable string       `json:"unreachable,omitempty"`
	Home        TargetRef    `json:"home,omitzero"`
	Targets     []TargetView `json:"targets"`
	Agents      []AgentView  `json:"agents,omitempty"`
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
}

type AgentView struct {
	Panel PanelID `json:"panel"`
	// Ref is the instance holding the panel. A panel id is unique only within
	// the process that numbers it, so the two together name one agent.
	Ref   TargetRef  `json:"ref,omitzero"`
	State AgentState `json:"state"`
}
