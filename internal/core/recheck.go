package core

import (
	"fmt"

	"github.com/hk9890/revier/pkg/revier"
)

// Recheck is the plan with each step's agents as a later survey finds them.
// It adds and drops no step: a plan confirmed is the plan that runs, and only
// what its agents do now can stop it (decisions.md D78).
//
// It errors on a step the survey could not answer for - the host that lists
// its instance did not answer, or its project is no longer surveyed. A
// degraded survey reports no agents, and no agents read is not idle: taking
// it as idle would let the close through in the one case it knows least.
func (c *Core) Recheck(r Report, plan []CloseStep) ([]CloseStep, error) {
	here := make(map[revier.ProjectName][]revier.AgentView, len(r.Views))
	for _, v := range r.Views {
		here[v.Project.Name] = c.agentsHere(v)
	}
	out := make([]CloseStep, len(plan))
	for i, step := range plan {
		if err := r.Failed[step.Ref.Host]; err != nil {
			return nil, fmt.Errorf("%s did not answer: %w", step.Ref.Host, err)
		}
		local, surveyed := here[step.Project]
		if !surveyed {
			return nil, fmt.Errorf("%s is no longer surveyed", step.Project)
		}
		step.Agents = agentsIn(local, step.Ref, step.panels())
		out[i] = step
	}
	return out, nil
}
