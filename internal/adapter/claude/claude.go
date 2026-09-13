// Package claude implements the AgentProbe for Claude Code.
//
// The state is Claude Code's own word, read from `claude agents --json`: every
// live session with the pid of its process and its status. A panel is matched
// to its session by pid and nothing else (decisions.md D55, D57).
//
// The listing costs a process, and a survey runs every second. So the probe
// keeps the last answer and runs the command again only when an answer may
// have changed: when a file under the sessions directory was written, added or
// removed since the last run, or when the answer is MaxAge old. Claude Code
// rewrites its session file on every status change. Only the files' times are
// read; their content is not an interface, and a Claude Code that stops
// touching them costs the state MaxAge of delay and nothing else.
//
// The pane title supplies the activity line only: the listing names a session
// but carries no summary of the turn.
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hk9890/revier/pkg/revier"
)

const (
	// glyphAtRest is U+2733, the marker Claude shows while it is not working.
	glyphAtRest = '✳'

	// defaultTitle is what Claude titles its pane before it has a summary of
	// the session to show, and what the launcher names the panel. It names
	// the tool, not what the tool is doing.
	defaultTitle = "Claude Code"
)

// MaxAge is the longest a listing is used without running the command again,
// for a change the sessions directory did not show.
const MaxAge = 10 * time.Second

// Probe reads Claude Code panels. The zero value is ready to use; it holds the
// last listing, so it is shared by pointer.
type Probe struct {
	// Now is injected so tests can pin Since and the listing's age. Nil means
	// time.Now.
	Now func() time.Time

	// Agents returns what `claude agents --json` prints. It is injected so a
	// test can answer without Claude Code installed. Nil runs the command.
	Agents func(ctx context.Context) ([]byte, error)

	// SessionsDir is where Claude Code keeps a file per live session. Empty
	// means $CLAUDE_CONFIG_DIR/sessions, or ~/.claude/sessions without it.
	SessionsDir string

	mu     sync.Mutex
	listed map[int]listedSession // by pid
	err    error
	at     time.Time
	stamp  map[string]time.Time // the sessions directory at the last run
}

// listedSession is one entry of `claude agents --json`. A background session
// has no pid, and no pane to be in.
type listedSession struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Status    string `json:"status"`
}

func (p *Probe) Name() string { return "claude" }

// Match recognises a Claude pane by its foreground command.
func (p *Probe) Match(panel revier.Panel) bool {
	return panel.Runs("claude") || panel.Runs("claude-code")
}

// Inspect reports the status the listing gives the panel's process. A panel
// with no session in it is unknown: Claude is running there, but revier
// cannot see what it does.
func (p *Probe) Inspect(ctx context.Context, panel revier.Panel) (revier.AgentState, error) {
	listed, err := p.listing(ctx)
	if err != nil {
		return revier.AgentState{}, err
	}
	state := revier.AgentState{Harness: "claude", Activity: Activity(panel.Title), Since: p.now()}
	if s, ok := listed[panel.PID]; ok && panel.PID != 0 {
		state.Status = Status(s.Status)
	}
	return state, nil
}

// Status maps a listed status. A value Claude Code adds later is unknown, not
// idle: a new status is more likely a new way to wait than a new way to rest.
func Status(listed string) revier.Status {
	switch listed {
	case "busy":
		return revier.StatusRunning
	case "waiting":
		return revier.StatusAttention
	case "idle":
		return revier.StatusIdle
	}
	return revier.StatusUnknown
}

// listing returns the last listing, running the command again when the
// sessions directory changed or the listing is MaxAge old. The directory is
// read before the command runs, so a change made while it runs is seen by the
// next call.
func (p *Probe) listing(ctx context.Context) (map[int]listedSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	stamp := p.readStamp()
	if !p.at.IsZero() && p.now().Sub(p.at) < MaxAge && sameStamp(stamp, p.stamp) {
		return p.listed, p.err
	}
	p.listed, p.err = p.list(ctx)
	p.at, p.stamp = p.now(), stamp
	return p.listed, p.err
}

// readStamp is the modification time of every file in the sessions directory.
// A directory that cannot be read is empty, which leaves MaxAge to notice.
func (p *Probe) readStamp() map[string]time.Time {
	entries, err := os.ReadDir(p.sessionsDir())
	if err != nil {
		return nil
	}
	stamp := make(map[string]time.Time, len(entries))
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			stamp[e.Name()] = info.ModTime()
		}
	}
	return stamp
}

func sameStamp(a, b map[string]time.Time) bool {
	if len(a) != len(b) {
		return false
	}
	for name, t := range a {
		if u, ok := b[name]; !ok || !u.Equal(t) {
			return false
		}
	}
	return true
}

func (p *Probe) sessionsDir() string {
	if p.SessionsDir != "" {
		return p.SessionsDir
	}
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "sessions")
}

func (p *Probe) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// list runs the command once and indexes its answer by pid.
func (p *Probe) list(ctx context.Context) (map[int]listedSession, error) {
	run := p.Agents
	if run == nil {
		run = claudeAgents
	}
	out, err := run(ctx)
	if err != nil {
		return nil, err
	}
	var listed []listedSession
	if err := json.Unmarshal(out, &listed); err != nil {
		return nil, fmt.Errorf("claude agents --json: %w", err)
	}
	byPID := make(map[int]listedSession, len(listed))
	for _, s := range listed {
		if s.PID != 0 {
			byPID[s.PID] = s
		}
	}
	return byPID, nil
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
// It always runs the command rather than reuse the listing Inspect keeps: a
// save records what holds now, and runs once.
func (p *Probe) Sessions(ctx context.Context, panels []revier.Panel) ([]revier.SessionID, error) {
	listed, err := p.list(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]revier.SessionID, len(panels))
	for i, panel := range panels {
		if panel.PID != 0 {
			ids[i] = revier.SessionID(listed[panel.PID].SessionID)
		}
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
