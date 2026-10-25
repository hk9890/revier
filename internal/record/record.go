// Package record writes and reads revier's state record: what revier did and
// what a surface saw, one JSON line each, appended to a file per day under
// $STATE/record and never pruned (decisions.md D151). `revier state` replays
// it to say what was open at a moment.
//
// It is kept apart from the event file (decisions.md D112): a reader counts
// those lines as uses, and a failure or a closed window is no use of a
// project. An operation records what it did, failed or not; only a surface
// records what it saw, as the difference between two of its own surveys
// (decisions.md D121).
//
// Like the log it is process-wide, and a process that never called Setup
// records nothing.
package record

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// By says whose a line is: an operation of the writing process did it, or a
// surface's survey saw it.
const (
	ByDid = "did"
	BySaw = "saw"
)

// The kinds a survey writes, as "saw" lines.
const (
	KindSnapshot        = "snapshot"
	KindInstanceOpened  = "instance opened"
	KindInstanceClosed  = "instance closed"
	KindTargetOpened    = "target opened"
	KindTargetClosed    = "target closed"
	KindAgentOpened     = "agent opened"
	KindAgentClosed     = "agent closed"
	KindAgentSession    = "agent session"
	KindHostUnreachable = "host unreachable"
	KindHostBack        = "host back"
	KindSurvey          = "survey"
)

// The kinds an operation writes, as "did" lines, each with err when it
// failed.
const (
	KindGo              = "go"
	KindGoAgent         = "go agent"
	KindAgentNew        = "agent new"
	KindShellNew        = "shell new"
	KindAction          = "action"
	KindAttach          = "attach"
	KindClose           = "close"
	KindShutdown        = "shutdown"
	KindShutdownRefused = "shutdown refused"
	KindRestoreStep     = "restore step"
	KindSessionRestored = "session restored"
	KindSessionSaved    = "session saved"
	KindProjectCreated  = "project created"
	KindProjectLinked   = "project linked"
	KindProjectRenamed  = "project renamed"
	KindProjectDeleted  = "project deleted"
	KindProjectSelected = "project selected"
	KindBound           = "bound"
	KindUnbound         = "unbound"
	KindAttached        = "attached"
	KindDetached        = "detached"
)

// Line is one line of the record. Machine is the linked host a line is
// about, empty for this machine; Host is the revier host (kitty, gnome,
// tmux) and Ref the instance on it. Pid and Cmd are the log's, so a line
// joins with the log of the process that wrote it.
type Line struct {
	Time     time.Time          `json:"time"`
	Kind     string             `json:"kind"`
	By       string             `json:"by"`
	Pid      int                `json:"pid,omitempty"`
	Cmd      string             `json:"cmd,omitempty"`
	Machine  string             `json:"machine,omitempty"`
	Host     string             `json:"host,omitempty"`
	Ref      string             `json:"ref,omitempty"`
	Title    string             `json:"title,omitempty"`
	Project  revier.ProjectName `json:"project,omitempty"`
	Target   revier.TargetName  `json:"target,omitempty"`
	Attached bool               `json:"attached,omitempty"`
	Tab      bool               `json:"tab,omitempty"`
	Panel    revier.PanelID     `json:"panel,omitempty"`
	Harness  string             `json:"harness,omitempty"`
	Session  revier.SessionID   `json:"session,omitempty"`
	Dir      string             `json:"dir,omitempty"`
	Agent    string             `json:"agent,omitempty"`
	Launched bool               `json:"launched,omitempty"`
	Action   string             `json:"action,omitempty"`
	// From is the name a rename or a selection left.
	From string `json:"from,omitempty"`
	// Saved is the session a save wrote or a restore opened. A save's own
	// snapshot carries it too.
	Saved    string            `json:"saved,omitempty"`
	Reason   session.Reason    `json:"reason,omitempty"`
	Err      string            `json:"err,omitempty"`
	Counts   map[string]int    `json:"counts,omitempty"`
	Agents   []Outcome         `json:"agents,omitempty"`
	Projects []session.Project `json:"projects,omitempty"`
	Seen     *Seen             `json:"seen,omitempty"`
}

// Failed is the line with why its operation failed, or the line as it is
// when err is nil.
func (l Line) Failed(err error) Line {
	if err != nil {
		l.Err = err.Error()
	}
	return l
}

// Outcome is what a restore step did with one agent.
type Outcome struct {
	Harness string           `json:"harness,omitempty"`
	Session revier.SessionID `json:"session,omitempty"`
	Dir     string           `json:"dir,omitempty"`
	Agent   string           `json:"agent,omitempty"`
	Outcome string           `json:"outcome,omitempty"`
}

// Seen is what one survey saw of one scope: this machine when Machine is
// empty, else one linked host. Projects is every project loaded here, or for
// a linked host the links to it. Hosts is the hosts that listed; Unlisted
// says why each other one did not, Unreachable why the linked host gave no
// answer, and Unknown why it gave none for a link. What they hold is not in
// the survey, and is not gone (decisions.md D89).
type Seen struct {
	Machine string `json:"machine,omitempty"`
	// Current is the project selected here, so a replay that starts at a
	// snapshot knows it without the line that selected it.
	Current     revier.ProjectName            `json:"current,omitempty"`
	Projects    []revier.ProjectName          `json:"projects,omitempty"`
	Hosts       []string                      `json:"hosts,omitempty"`
	Unlisted    map[string]string             `json:"unlisted,omitempty"`
	Unreachable string                        `json:"unreachable,omitempty"`
	Unknown     map[revier.ProjectName]string `json:"unknown,omitempty"`
	Instances   []Instance                    `json:"instances,omitempty"`
	Targets     []Target                      `json:"targets,omitempty"`
	Agents      []Agent                       `json:"agents,omitempty"`
}

// Instance is one window, terminal or session a host listed.
type Instance struct {
	Host  string `json:"host"`
	Ref   string `json:"ref"`
	Title string `json:"title,omitempty"`
}

// Target is a target open on an instance, or an instance attached to a
// project by hand, which has no target name. Tab is a tab target that comes
// back with the instance that holds it, so a save records no step for it.
type Target struct {
	Project  revier.ProjectName `json:"project,omitempty"`
	Target   revier.TargetName  `json:"target,omitempty"`
	Host     string             `json:"host"`
	Ref      string             `json:"ref"`
	Attached bool               `json:"attached,omitempty"`
	Tab      bool               `json:"tab,omitempty"`
}

// Agent is an agent in a panel of an instance. Agent is the named agent the
// panel runs.
type Agent struct {
	Project revier.ProjectName `json:"project"`
	Host    string             `json:"host,omitempty"`
	Ref     string             `json:"ref,omitempty"`
	Panel   revier.PanelID     `json:"panel,omitempty"`
	Harness string             `json:"harness,omitempty"`
	Session revier.SessionID   `json:"session,omitempty"`
	Dir     string             `json:"dir,omitempty"`
	Agent   string             `json:"agent,omitempty"`
}

// Dir is where the day files are under the state root.
func Dir(stateRoot string) string { return filepath.Join(stateRoot, "record") }

// day is the name of the file a line at t goes to: the date where t was
// taken, so a line and the file it is in agree on the day.
func day(t time.Time) string { return t.Format(time.DateOnly) }

var (
	mu       sync.Mutex
	dir      string
	pid      int
	cmd      string
	now      = time.Now
	observer = NewObserver()
)

// Setup makes the record under stateRoot the one this process writes to,
// with cmd as the log names the process. With no root the process records
// nothing again, which is how a test leaves it.
func Setup(stateRoot, command string) {
	mu.Lock()
	defer mu.Unlock()
	dir, pid, cmd, observer = "", os.Getpid(), command, NewObserver()
	if stateRoot != "" {
		dir = Dir(stateRoot)
	}
}

// Write appends the lines, each stamped with this process and with the time
// when it carries none. A line that cannot be written costs that line, not
// the operation it is of.
func Write(lines ...Line) {
	mu.Lock()
	defer mu.Unlock()
	write(lines)
}

// Saw records what a surface's survey saw of each scope: the difference from
// its last survey of that scope, a snapshot, or nothing (Observer.Saw).
func Saw(seen ...Seen) {
	mu.Lock()
	defer mu.Unlock()
	if dir == "" {
		return
	}
	at := now()
	for _, s := range seen {
		write(observer.Saw(s, at))
	}
}

func write(lines []Line) {
	if dir == "" {
		return
	}
	for _, l := range lines {
		if l.Time.IsZero() {
			l.Time = now()
		}
		l.Pid, l.Cmd = pid, cmd
		logging.Repeat("record", "record", appendLine(filepath.Join(dir, day(l.Time)+".jsonl"), l), "kind", l.Kind)
	}
}

// appendLine writes the line as one write: O_APPEND keeps the lines of
// concurrent processes whole on a local file system.
func appendLine(path string, l Line) error {
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return errors.Join(err, f.Close())
}

// Read returns what a replay at at needs, sorted by time: the lines of at's
// day file, and of each earlier one back to the first that holds a snapshot
// of this machine at or before at. With none, it reads every file. No
// directory is no lines. A line that is not one is skipped and logged: the
// files are appended to by every process, and one bad line must not cost the
// rest.
func Read(stateRoot string, at time.Time) ([]Line, error) {
	entries, err := os.ReadDir(Dir(stateRoot))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var days []string
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".jsonl")
		if ok && !e.IsDir() && name <= day(at) {
			days = append(days, name)
		}
	}
	slices.Sort(days)
	var out []Line
	for i := len(days) - 1; i >= 0; i-- {
		lines, err := readDay(filepath.Join(Dir(stateRoot), days[i]+".jsonl"))
		if err != nil {
			return nil, err
		}
		out = append(lines, out...)
		if slices.ContainsFunc(lines, func(l Line) bool { return snapshotHere(l) && !l.Time.After(at) }) {
			break
		}
	}
	slices.SortStableFunc(out, func(a, b Line) int { return a.Time.Compare(b.Time) })
	return out, nil
}

func snapshotHere(l Line) bool { return l.Kind == KindSnapshot && l.Machine == "" }

func readDay(path string) ([]Line, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Line
	n := 0
	for line := range bytes.Lines(data) {
		n++
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var l Line
		if err := json.Unmarshal(line, &l); err != nil {
			slog.Warn("record line skipped", "file", filepath.Base(path), "line", n, "err", err)
			continue
		}
		out = append(out, l)
	}
	return out, nil
}
