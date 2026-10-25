package config

import (
	"fmt"

	"github.com/hk9890/revier/internal/core"
)

// Terminal is the [terminal] table: the keys the terminal holds for revier,
// and what they do in a terminal window that is no project's workspace. A
// key left out has its default; one written empty is no key.
type Terminal struct {
	ShellKey *string `toml:"shell_key"`
	AgentKey *string `toml:"agent_key"`
	Outside  string  `toml:"outside"`
}

// The defaults of [terminal]. The shell and agent keys are the chords the
// guide's hand-written terminal maps used.
const (
	DefaultShellKey = "ctrl+shift+t"
	DefaultAgentKey = "ctrl+shift+z"
)

// What a terminal key does outside a workspace: open a plain tab beside the
// window, or nothing.
const (
	OutsideTab  = "tab"
	OutsideNone = "none"
)

// TermKey is one terminal key config.toml declares, its chord parsed: the
// shell's, the default agent's, or a named agent's.
type TermKey struct {
	Chord core.Chord
	// Shell is the key that opens a shell. Agent names the named agent the
	// key opens; empty, with Shell false, it is the default agent.
	Shell bool
	Agent string
}

// TermKeys is the terminal keys config.toml declares and no rule refused:
// the shell's, the default agent's, then each named agent's, in file order.
func (c *Config) TermKeys() []TermKey { return c.termKeys }

// Outside is what a terminal key does outside a workspace: OutsideTab or
// OutsideNone.
func (c *Config) Outside() string {
	if c.Terminal.Outside == "" {
		return OutsideTab
	}
	return c.Terminal.Outside
}

// validateTerminal takes the terminal keys into termKeys and is why each one
// left out was refused: a chord that does not parse, or one an earlier key
// holds already. A value of outside that is neither choice is the default,
// and a problem. refused is validateAgents' answer for c.Agents: a refused
// table's key is no key, and its refusal says so already.
func (c *Config) validateTerminal(refused []error) []error {
	var errs []error
	switch c.Terminal.Outside {
	case "", OutsideTab, OutsideNone:
	default:
		errs = append(errs, fmt.Errorf("terminal.outside: %q is neither %q nor %q", c.Terminal.Outside, OutsideTab, OutsideNone))
		c.Terminal.Outside = ""
	}
	held := map[core.Chord]string{}
	take := func(what, raw string, k TermKey) {
		if raw == "" {
			return
		}
		chord, err := core.ParseChord(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", what, err))
			return
		}
		if prev, ok := held[chord]; ok {
			errs = append(errs, fmt.Errorf("%s: key %q is held by %s already", what, chord, prev))
			return
		}
		held[chord] = what
		k.Chord = chord
		c.termKeys = append(c.termKeys, k)
	}
	orDefault := func(v *string, def string) string {
		if v == nil {
			return def
		}
		return *v
	}
	take("terminal.shell_key", orDefault(c.Terminal.ShellKey, DefaultShellKey), TermKey{Shell: true})
	take("terminal.agent_key", orDefault(c.Terminal.AgentKey, DefaultAgentKey), TermKey{})
	for i, t := range c.Agents {
		if refused[i] != nil {
			continue
		}
		name, _ := t["name"].(string)
		key, _ := t["key"].(string)
		take(fmt.Sprintf("agent %q key", name), key, TermKey{Agent: name})
	}
	return errs
}
