// Package panelstart words what the runtime hosts, tmux and herdr, say about a
// panel whose command does not start, so that both say it the same way.
package panelstart

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// Startable refuses panels whose program is not on PATH before a runtime
// starts any of them. The runtime starts it anyway, the pane closes at once,
// and what the runtime reports names neither the panel nor its command. A
// program named by a path is left to the runtime: it is read against the
// panel's directory, not this one.
func Startable(panels []revier.PanelSpec) error {
	for _, p := range panels {
		if len(p.Command) == 0 || strings.Contains(p.Command[0], "/") {
			continue
		}
		if _, err := exec.LookPath(p.Command[0]); err != nil {
			return fmt.Errorf("%s command %q is not on PATH", whose(p), p.Command[0])
		}
	}
	return nil
}

// ExitedAtOnce says the panel's command ended as soon as it started.
func ExitedAtOnce(p revier.PanelSpec) error {
	if len(p.Command) == 0 {
		return fmt.Errorf("%s shell exited as soon as it started", whose(p))
	}
	return fmt.Errorf("%s command %q exited as soon as it started", whose(p), strings.Join(p.Command, " "))
}

// whose names what a command belongs to: a panel by its kind. A realization's
// launch has no kind, and neither has a panel whose kind was left out, so
// either is named by its command alone.
func whose(p revier.PanelSpec) string {
	if p.Kind == "" {
		return "the"
	}
	return "the " + string(p.Kind) + " panel's"
}
