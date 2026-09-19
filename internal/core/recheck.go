package core

import (
	"slices"

	"github.com/hk9890/revier/pkg/revier"
)

// Recheck is the plan with each step's agents as a later survey finds them.
// It adds and drops no step: a plan confirmed is the plan that runs, and only
// what its agents do now can stop it (decisions.md D78).
func (c *Core) Recheck(r Report, plan []CloseStep) []CloseStep {
	out := make([]CloseStep, len(plan))
	for i, step := range plan {
		var local []revier.AgentView
		if j := slices.IndexFunc(r.Views, func(v revier.ProjectView) bool { return v.Project.Name == step.Project }); j >= 0 {
			local = c.agentsHere(r.Views[j])
		}
		step.Agents = agentsIn(local, step.Ref, step.Panel)
		out[i] = step
	}
	return out
}
