// Package events writes and reads revier's event file: one JSON line per
// thing revier did in a project, appended to events.jsonl under the state root
// and never pruned (decisions.md D112). The log beside it is for diagnosis and
// is gone after two weeks; this file is what `revier events` prints, and what
// a count of how a project was used is made from.
//
// Like the log it is process-wide: a package that has an event calls Record,
// and a process that never called Setup records nothing.
package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hk9890/revier/pkg/revier"
)

// Path is where the file is.
func Path(stateRoot string) string { return filepath.Join(stateRoot, "events.jsonl") }

var (
	mu   sync.Mutex
	file string
	// seen is the conversations recorded on day. It starts empty on each new
	// day, so a surface left open for months holds one day of them.
	day  string
	seen = map[sighting]bool{}
	now  = time.Now
)

// Setup makes the file under stateRoot the one this process records to. With
// no root the process records nothing again, which is how a test leaves it.
func Setup(stateRoot string) {
	mu.Lock()
	defer mu.Unlock()
	file, day, seen = "", "", map[sighting]bool{}
	if stateRoot != "" {
		file = Path(stateRoot)
	}
}

// Record appends one event, stamped with the time when it carries none. An
// event that cannot be written costs the record, not the operation it is of.
func Record(e revier.Event) {
	mu.Lock()
	defer mu.Unlock()
	record(e)
}

func record(e revier.Event) {
	if file == "" {
		return
	}
	if e.Time.IsZero() {
		e.Time = now()
	}
	if err := appendLine(file, e); err != nil {
		slog.Warn("event", "event", e.Kind, "project", e.Project, "err", err)
	}
}

// appendLine writes the event as one write of one line: O_APPEND keeps the
// lines of concurrent processes whole.
func appendLine(path string, e revier.Event) error {
	line, err := json.Marshal(e)
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
	_, err = f.Write(append(line, '\n'))
	return errors.Join(err, f.Close())
}

// sighting is one conversation seen in one project.
type sighting struct {
	project revier.ProjectName
	session revier.SessionID
}

// Sessions records the conversation each surveyed agent holds, as an
// EventAgentSession: once per process when it is first seen, and once more on
// each later day it is seen again, so a read of the last days finds every
// conversation that was alive in them. It is called every refresh, and writes
// only for what is new.
//
// The directory is the agent's own, else the project's: a view names an
// agent's directory only when it is not the project's.
func Sessions(views []revier.ProjectView) {
	mu.Lock()
	defer mu.Unlock()
	if file == "" {
		return
	}
	at := now()
	if today := at.Format(time.DateOnly); today != day {
		day, seen = today, map[sighting]bool{}
	}
	for _, v := range views {
		for _, a := range v.Agents {
			if a.State.Session == "" {
				continue
			}
			key := sighting{project: v.Project.Name, session: a.State.Session}
			if seen[key] {
				continue
			}
			seen[key] = true
			dir := a.State.Dir
			if dir == "" && v.Project.Remote == nil {
				dir = v.Project.Path
			}
			record(revier.Event{
				Time: at, Kind: revier.EventAgentSession, Project: v.Project.Name,
				Agent: a.State.Harness, Session: a.State.Session, Dir: dir,
			})
		}
	}
}

// Read returns the events recorded at since or later, in the order they were
// written. No file is no events. A line that is not an event, of whatever
// length, is skipped and logged: the file is appended to by every process,
// for years, and one bad line must not cost the rest.
func Read(stateRoot string, since time.Time) ([]revier.Event, error) {
	data, err := os.ReadFile(Path(stateRoot))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []revier.Event
	n := 0
	for line := range bytes.Lines(data) {
		n++
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var e revier.Event
		if err := json.Unmarshal(line, &e); err != nil {
			slog.Warn("event line skipped", "line", n, "err", err)
			continue
		}
		if !e.Time.Before(since) {
			out = append(out, e)
		}
	}
	return out, nil
}
