package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/pkg/revier"
)

// ErrNoScreen means nothing here can say what the agent's panel shows: the
// runtime cannot read a panel, or no panel of this machine's runtime shows
// the agent. It is a normal outcome, and the mirror says so.
var ErrNoScreen = errors.New("the agent's panel cannot be read")

// Screen is what the panel that shows the agent shows, with its colours, and
// with its scrollback when asked: what Mirror cuts, and what `revier agent
// read --screen` prints whole as text (decisions.md D116). It is the panel
// here, so a link's agent is read as this machine's terminal draws it and
// nothing is asked of its host.
func (c *Core) Screen(ctx context.Context, a revier.AgentView, scrollback bool) (string, error) {
	r, ok := c.Runtime.(revier.PanelReader)
	if !ok || !c.here(a) {
		return "", fmt.Errorf("agent %s: %w", a.Panel, ErrNoScreen)
	}
	return r.ReadPanel(ctx, a.Ref, a.Panel, scrollback)
}

// Mirror is the agent's screen as both panes show it (decisions.md D111,
// D125): Screen, cut above the frame the agent's harness draws under what the
// agent said, when its probe can find it as a revier.Framed (D136). The probe
// is the one that named the agent's harness, so a link's agent is cut as its
// host's probe would cut it.
func (c *Core) Mirror(ctx context.Context, a revier.AgentView, scrollback bool) (string, error) {
	text, err := c.Screen(ctx, a, scrollback)
	if err != nil {
		return "", err
	}
	for _, probe := range c.Probes {
		framed, ok := probe.(revier.Framed)
		if !ok || probe.Name() != a.State.Harness {
			continue
		}
		lines := strings.Split(text, "\n")
		plain := make([]string, len(lines))
		for i, l := range lines {
			plain[i] = ansi.Strip(l)
		}
		return strings.Join(lines[:framed.Frame(plain)], "\n"), nil
	}
	return text, nil
}
