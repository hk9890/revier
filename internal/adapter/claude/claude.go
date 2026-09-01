// Package claude implements the AgentProbe for Claude Code.
//
// Two signals reach a panel, and they are not equally trustworthy.
//
// The live window title carries a leading state glyph. That glyph is a LEVEL:
// it describes what Claude is doing right now, it is repainted continuously,
// and it arrives on remote and container panes alike.
//
// The CS_STATE user variable is set by Claude Code's own hooks
// (UserPromptSubmit/Stop/Notification). It records EDGES, not levels: nothing
// clears "attn" until the next Stop, and "busy" survives a turn interrupted
// with Esc, so a pane sitting at rest can carry "busy" indefinitely. Read as a
// level it reports work on an idle pane.
//
// So the glyph decides running versus not-running, and CS_STATE is consulted
// only for the one thing the glyph cannot express: that Claude asked for the
// human and has not been answered. An unrecognised glyph degrades to idle, so
// a future Claude change can only understate activity rather than assert a
// wrong state.
//
// This mirrors the rule proven in the shell implementation this replaces
// (dotfiles/kitty/.config/kitty/watchers/cs_tab_title.py in the setup repo).
package claude

import (
	"context"
	"strings"
	"time"

	"github.com/hk9890/revier/pkg/revier"
)

const (
	// varTab marks a pane as a Claude agent. kt-new-agent.sh emits it as a
	// SetUserVar OSC before exec, so it reaches the local terminal for native,
	// container, and SSH panes alike.
	varTab = "CS_TAB"
	// varState is the hook-set edge record. Advisory only; see the package doc.
	varState = "CS_STATE"

	// stateAttention is the value the Notification hook sets when Claude wants
	// the human. It is the only CS_STATE value this probe acts on.
	stateAttention = "attn"

	// glyphAtRest is U+2733, the marker Claude shows while it is not working.
	glyphAtRest = '✳'
)

// Probe reads Claude Code panels.
type Probe struct {
	// Now is injected so tests can pin Since. Nil means time.Now.
	Now func() time.Time
}

func (p *Probe) Name() string { return "claude" }

// Match recognises a Claude pane by its marker variable, falling back to the
// foreground command for a pane started outside the marked launcher.
func (p *Probe) Match(panel revier.Panel) bool {
	if panel.Vars[varTab] == "1" {
		return true
	}
	for _, arg := range panel.Command {
		base := arg[strings.LastIndex(arg, "/")+1:]
		if base == "claude" || base == "claude-code" {
			return true
		}
	}
	return false
}

// Inspect derives the state. It performs no I/O: everything it needs is
// already on the panel, which is what makes the whole probe a pure function
// and testable at layer L1.
func (p *Probe) Inspect(_ context.Context, panel revier.Panel) (revier.AgentState, error) {
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}

	state := revier.AgentState{Harness: "claude", Activity: Activity(panel.Title), Since: now()}
	switch {
	case IsSpinner(leading(panel.Title)):
		state.Status = revier.StatusRunning
	case panel.Vars[varState] == stateAttention:
		state.Status = revier.StatusAttention
	default:
		state.Status = revier.StatusIdle
	}
	return state, nil
}

// leading returns the first rune of a title, or 0 when it is empty.
func leading(title string) rune {
	for _, r := range title {
		return r
	}
	return 0
}

// IsSpinner reports the glyphs Claude shows only while it is working: the
// braille-pattern block, and the half-filled circles U+25D0..U+25D3.
func IsSpinner(r rune) bool {
	return (r >= 0x2800 && r <= 0x28FF) || (r >= 0x25D0 && r <= 0x25D3)
}

// IsStateGlyph reports any leading state marker, working or at rest.
func IsStateGlyph(r rune) bool { return IsSpinner(r) || r == glyphAtRest }

// Activity strips a leading state glyph and the space after it, leaving the
// summary Claude wrote. The glyph is rendered separately, so leaving it in
// would show the state twice.
func Activity(title string) string {
	if r := leading(title); IsStateGlyph(r) {
		return strings.TrimLeft(strings.TrimPrefix(title, string(r)), " ")
	}
	return title
}
