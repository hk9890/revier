// Package opencode implements the AgentProbe for opencode, as far as opencode
// lets a panel see, which is presence and nothing more.
//
// What was checked, on opencode 1.18.25 in kitty 0.48 on 2026-09-02, before
// deciding the probe reports no state:
//
//   - The window title. opencode sets it to "OpenCode" once at start and never
//     repaints it: it read "OpenCode" at the idle prompt and throughout a turn
//     submitted with send-text. There is no glyph and no level to read.
//   - Terminal user variables. `kitten @ ls` reported an empty user_vars for
//     the pane at every point. opencode has no hook that sets one, and the
//     launcher in the setup repository marks only Claude panes (CS_TAB).
//   - The process. opencode's TUI talks to a server it starts on a private
//     port, which a panel does not expose; nothing in the foreground process
//     list changes between idle and working.
//
// So Match recognises an opencode panel by its foreground command and Inspect
// reports StatusUnknown with the harness name. The monitor then shows that an
// opencode agent is present, which is true, rather than idle or running, which
// would be a guess. A wrong status is worse than an absent one.
package opencode

import (
	"context"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// Probe reads opencode panels.
type Probe struct{}

func (p *Probe) Name() string { return "opencode" }

// Match recognises an opencode pane by its foreground command.
func (p *Probe) Match(panel revier.Panel) bool {
	for _, arg := range panel.Command {
		if arg[strings.LastIndex(arg, "/")+1:] == "opencode" {
			return true
		}
	}
	return false
}

// Inspect reports presence only. See the package comment for what opencode
// does not expose.
func (p *Probe) Inspect(_ context.Context, _ revier.Panel) (revier.AgentState, error) {
	return revier.AgentState{Harness: "opencode", Status: revier.StatusUnknown}, nil
}
