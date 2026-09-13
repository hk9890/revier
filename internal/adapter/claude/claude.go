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
	"encoding/json"
	"fmt"
	"os/exec"
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

	// defaultTitle is what Claude titles its pane before it has a summary of
	// the session to show, and what the launcher names the panel. It names
	// the tool, not what the tool is doing.
	defaultTitle = "Claude Code"
)

// Probe reads Claude Code panels.
type Probe struct {
	// Now is injected so tests can pin Since. Nil means time.Now.
	Now func() time.Time

	// Agents returns what `claude agents --json` prints. It is injected so a
	// test can answer without Claude Code installed. Nil runs the command.
	Agents func(ctx context.Context) ([]byte, error)
}

func (p *Probe) Name() string { return "claude" }

// Match recognises a Claude pane by its marker variable, falling back to the
// foreground command for a pane started outside the marked launcher.
func (p *Probe) Match(panel revier.Panel) bool {
	return panel.Vars[varTab] == "1" || panel.Runs("claude") || panel.Runs("claude-code")
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

// Sessions names the conversation each pane holds, from one run of `claude
// agents --json`: Claude Code's scripting interface, which lists every active
// interactive session with the pid of its process. A pane is matched by that
// pid and nothing else, so two agents in one repository are told apart - the
// case this feature exists for, and the one a guess from the newest transcript
// under ~/.claude/projects gets wrong.
//
// The pid is the Claude process itself. kitty reports the foreground process,
// and tmux the pane's own process, which is Claude when it is the pane's
// command - as revier launches it. A pane where claude was typed into a shell
// reports the shell, is not matched, and restores empty.
//
// Both pids are of live processes, listed now, so a pid reused since cannot
// match a conversation that is not there.
func (p *Probe) Sessions(ctx context.Context, panels []revier.Panel) ([]revier.SessionID, error) {
	run := p.Agents
	if run == nil {
		run = claudeAgents
	}
	out, err := run(ctx)
	if err != nil {
		return nil, err
	}
	var listed []struct {
		PID       int    `json:"pid"`
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(out, &listed); err != nil {
		return nil, fmt.Errorf("claude agents --json: %w", err)
	}
	byPID := make(map[int]revier.SessionID, len(listed))
	for _, a := range listed {
		// A background session has no pid, and no pane to be in.
		if a.PID != 0 && a.SessionID != "" {
			byPID[a.PID] = revier.SessionID(a.SessionID)
		}
	}
	ids := make([]revier.SessionID, len(panels))
	for i, panel := range panels {
		ids[i] = byPID[panel.PID]
	}
	return ids, nil
}

func claudeAgents(ctx context.Context) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "claude", "agents", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("claude agents --json: %w", err)
	}
	return out, nil
}

// ResumeCommand folds --resume into the panel as it is configured now, so a
// project that runs its agent with a model flag keeps the flag. A spec with no
// command of its own is the bare harness, which is what the launcher runs.
func (p *Probe) ResumeCommand(spec revier.PanelSpec, id revier.SessionID) []string {
	cmd := spec.Command
	if len(cmd) == 0 {
		cmd = []string{"claude"}
	}
	return append(append([]string{}, cmd...), "--resume", string(id))
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
// would show the state twice. Claude's default title is no summary at all,
// and reads as none: shown, it put "Claude Code" on every idle agent's row.
func Activity(title string) string {
	if r := leading(title); IsStateGlyph(r) {
		title = strings.TrimLeft(strings.TrimPrefix(title, string(r)), " ")
	}
	if title == defaultTitle {
		return ""
	}
	return title
}
