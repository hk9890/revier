package claude

import (
	"context"
	"time"
)

// SetNow replaces the clock, so a test pins Since and the listing's age.
func (p *Probe) SetNow(now func() time.Time) { p.clock = now }

// SetAgents replaces the `claude agents --json` call, so a test answers
// without Claude Code installed.
func (p *Probe) SetAgents(agents func(ctx context.Context) ([]byte, error)) { p.agents = agents }
