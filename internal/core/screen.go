package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/hk9890/revier/pkg/revier"
)

// ErrNoScreen means nothing here can say what the agent's panel shows: the
// runtime cannot read a panel, or no panel of this machine's runtime shows
// the agent. It is a normal outcome, and the mirror says so.
var ErrNoScreen = errors.New("the agent's panel cannot be read")

// Screen is what the panel that shows the agent shows, with its colours, and
// with its scrollback when asked: the mirror of both panes (decisions.md
// D111, D123), and what `revier agent read --screen` prints as text (D116). It is
// the panel here, so a link's agent is read as this machine's terminal draws
// it and nothing is asked of its host.
func (c *Core) Screen(ctx context.Context, a revier.AgentView, scrollback bool) (string, error) {
	r, ok := c.Runtime.(revier.PanelReader)
	if !ok || !c.here(a) {
		return "", fmt.Errorf("agent %s: %w", a.Panel, ErrNoScreen)
	}
	return r.ReadPanel(ctx, a.Ref, a.Panel, scrollback)
}
