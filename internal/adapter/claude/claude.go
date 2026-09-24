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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	// clock is replaced by tests to pin the listing's age. Nil
	// means time.Now.
	clock func() time.Time

	// agents returns what `claude agents --json` prints. Tests replace it to
	// answer without Claude Code installed. Nil runs the command.
	agents func(ctx context.Context) ([]byte, error)

	// SessionsDir is where Claude Code keeps a file per live session. Empty
	// means $CLAUDE_CONFIG_DIR/sessions, or ~/.claude/sessions without it.
	SessionsDir string

	mu     sync.Mutex
	listed map[int]listedSession // by pid
	err    error
	at     time.Time
	stamp  map[string]time.Time // the sessions directory at the last run

	readsMu sync.Mutex
	reads   map[string]read   // the last read of each transcript, by path
	found   map[string]string // where a transcript not under its cwd was found, by session
}

// listedSession is one entry of `claude agents --json`. A background session
// has no pid, and no pane to be in.
type listedSession struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Status    string `json:"status"`
	Cwd       string `json:"cwd"`
}

func (p *Probe) Name() string { return "claude" }

// Activity is the title's activity line, for a panel whose process is not on
// this machine (revier.Titled).
func (p *Probe) Activity(title string) string { return Activity(title) }

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
	state := revier.AgentState{Harness: "claude", Activity: Activity(panel.Title)}
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
//
// A run the caller's context cut short is not kept: it says the caller ran
// out of time, not what Claude Code answers, and kept it would blank every
// agent until MaxAge.
func (p *Probe) listing(ctx context.Context) (map[int]listedSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	stamp := p.readStamp()
	if !p.at.IsZero() && p.now().Sub(p.at) < MaxAge && sameStamp(stamp, p.stamp) {
		return p.listed, p.err
	}
	listed, err := p.list(ctx)
	if err != nil && ctx.Err() != nil {
		return nil, err
	}
	p.listed, p.err = listed, err
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
	if p.clock != nil {
		return p.clock()
	}
	return time.Now()
}

// list runs the command once and indexes its answer by pid.
func (p *Probe) list(ctx context.Context) (map[int]listedSession, error) {
	run := p.agents
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
// The directory is the session's cwd as the listing gives it, which is where
// Claude works and not where its pane was opened: an agent that entered a
// worktree is listed in the worktree.
//
// It always runs the command rather than reuse the listing Inspect keeps: a
// save records what holds now, and runs once.
func (p *Probe) Sessions(ctx context.Context, panels []revier.Panel) ([]revier.Conversation, error) {
	listed, err := p.list(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]revier.Conversation, len(panels))
	for i, panel := range panels {
		if s, ok := listed[panel.PID]; ok && panel.PID != 0 && s.SessionID != "" {
			out[i] = revier.Conversation{ID: revier.SessionID(s.SessionID), Dir: s.Cwd}
		}
	}
	return out, nil
}

const (
	// listTimeout bounds one run of the listing, which answers in well under
	// a second. A caller's own deadline can be a minute - a CLI command's -
	// and one hung claude must not hold `revier list` that long.
	listTimeout = 5 * time.Second

	// waitDelay is how long the listing's output is waited for once its
	// context is done. Without it a child of claude that keeps stdout open
	// holds the command past its deadline, and the probe's lock with it.
	waitDelay = time.Second
)

func claudeAgents(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "agents", "--json")
	cmd.WaitDelay = waitDelay
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude agents --json: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// ResumeCommand folds --resume into the panel as it is configured now, so a
// project that runs its agent with a model flag keeps the flag. A spec with no
// command of its own is the bare harness, which is what the launcher runs.
//
// A flag that picks the conversation itself - --continue, --resume, or
// --session-id - is dropped first: it contradicts the recorded conversation,
// and Claude Code either refuses the pair or opens the other one.
func (p *Probe) ResumeCommand(spec revier.PanelSpec, id revier.SessionID) []string {
	cmd := spec.Command
	if len(cmd) == 0 {
		cmd = []string{"claude"}
	}
	return append(withoutConversation(cmd), "--resume", string(id))
}

// withoutConversation is a copy of argv without the flags that choose a
// conversation. Only the words after claude itself are Claude Code's flags:
// the -c of a `bash -c` wrapper is the shell's. A command that does not name
// claude is a wrapper that cannot be read, and is kept whole. --resume takes
// an optional value, so the word after it is its value unless it is a flag.
func withoutConversation(argv []string) []string {
	at := slices.IndexFunc(argv, func(a string) bool {
		base := filepath.Base(a)
		return base == "claude" || base == "claude-code"
	})
	if at < 0 {
		return slices.Clone(argv)
	}
	out := slices.Clone(argv[:at+1])
	for i := at + 1; i < len(argv); i++ {
		switch a := argv[i]; {
		case a == "-c" || a == "--continue":
		case a == "-r" || a == "--resume":
			if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
				i++
			}
		case a == "--session-id":
			i++
		case strings.HasPrefix(a, "--resume=") || strings.HasPrefix(a, "--session-id="):
		default:
			out = append(out, a)
		}
	}
	return out
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
