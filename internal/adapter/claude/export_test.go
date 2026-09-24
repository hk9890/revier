package claude

import (
	"context"
	"time"
)

// SetNow replaces the clock, so a test pins the listing's age.
func (p *Probe) SetNow(now func() time.Time) { p.clock = now }

// SetAgents replaces the `claude agents --json` call, so a test answers
// without Claude Code installed.
func (p *Probe) SetAgents(agents func(ctx context.Context) ([]byte, error)) { p.agents = agents }

// Cached is how many sessions the probe keeps a read and a look for, so a
// test can see that a session the listing dropped is forgotten.
func (p *Probe) Cached() (reads, sweeps int) {
	p.readsMu.Lock()
	defer p.readsMu.Unlock()
	return len(p.reads), len(p.found)
}
