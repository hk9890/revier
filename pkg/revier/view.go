package revier

// ProjectView is what the core produces and both renderers - the TUI and
// --json - consume. It is the only type they share, so a field added here
// serves both surfaces at once.
type ProjectView struct {
	Project Project      `json:"project"`
	Running bool         `json:"running"`
	Home    TargetRef    `json:"home,omitzero"`
	Targets []TargetView `json:"targets"`
	Agents  []AgentView  `json:"agents,omitempty"`
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
	Panel PanelID    `json:"panel"`
	State AgentState `json:"state"`
}
