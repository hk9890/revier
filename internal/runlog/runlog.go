// Package runlog keeps the record of every `revier each`: one directory per
// run under the state root, holding each project's output and a summary of how
// each project ended. `revier each log` reads it back.
//
// It lives beside state rather than inside it. State is one small document
// every keypress reads; a run's output is as large as whatever the command
// printed ninety times, and nothing on a keypress needs it.
package runlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// Status is how one project ended.
type Status string

const (
	StatusOK      Status = "ok"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

// Result is one project's line in a run.
type Result struct {
	Project revier.ProjectName `json:"project"`
	Path    string             `json:"path"`
	Status  Status             `json:"status"`
	Skip    core.Skip          `json:"skip,omitempty"`
	SameAs  revier.ProjectName `json:"same_as,omitempty"`
	Exit    int                `json:"exit,omitempty"`
	// Error is why the command did not start at all, which has no exit
	// status to report.
	Error string `json:"error,omitempty"`
	// Output is the file, inside the run's directory, that holds everything
	// the command printed.
	Output  string  `json:"output,omitempty"`
	Seconds float64 `json:"seconds,omitempty"`
}

// Run is one `revier each`. It is saved after every project, so a run that was
// interrupted still says how far it got; Finished is zero until it ends.
type Run struct {
	ID       string    `json:"id"`
	Argv     []string  `json:"argv"`
	Filter   string    `json:"filter,omitempty"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitzero"`
	Results  []Result  `json:"results"`

	// Dir is where the run's files are. It is derived from where the run was
	// found, so it is not stored.
	Dir string `json:"-"`
}

// Count reports how many results ended with the status.
func (r Run) Count(s Status) int {
	n := 0
	for _, res := range r.Results {
		if res.Status == s {
			n++
		}
	}
	return n
}

// Dir is the directory that holds every run.
func Dir(stateRoot string) string { return filepath.Join(stateRoot, "runs") }

const summaryFile = "summary.json"

// Start creates the directory for a new run and saves its empty summary. The
// id is the start time, so a listing sorted by name is sorted by age, and the
// process id, so two runs started in the same second do not collide.
func Start(stateRoot string, argv []string, filter string, now time.Time) (*Run, error) {
	id := fmt.Sprintf("%s-%d", now.Format("2006-01-02T15-04-05"), os.Getpid())
	r := &Run{ID: id, Argv: argv, Filter: filter, Started: now, Results: []Result{}, Dir: filepath.Join(Dir(stateRoot), id)}
	if err := os.MkdirAll(Dir(stateRoot), 0o755); err != nil {
		return nil, fmt.Errorf("create run directory: %w", err)
	}
	if err := os.Mkdir(r.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("create run directory: %w", err)
	}
	return r, r.Save()
}

// Output creates the file a project's output goes to and returns it with the
// name Result.Output records.
func (r *Run) Output(p revier.ProjectName) (*os.File, string, error) {
	name := strings.ReplaceAll(string(p), string(filepath.Separator), "_") + ".log"
	f, err := os.Create(filepath.Join(r.Dir, name))
	if err != nil {
		return nil, "", fmt.Errorf("create output for %s: %w", p, err)
	}
	return f, name, nil
}

// Save writes the summary atomically, so an interrupt mid-write leaves the
// previous one rather than a truncated file.
func (r *Run) Save() error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encode run: %w", err)
	}
	tmp, err := os.CreateTemp(r.Dir, "summary-*.json")
	if err != nil {
		return fmt.Errorf("create temp summary: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp summary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp summary: %w", err)
	}
	return os.Rename(tmp.Name(), filepath.Join(r.Dir, summaryFile))
}

// ErrNoRun is returned for an id that names no run.
var ErrNoRun = errors.New("no such run")

// Load reads one run.
func Load(stateRoot, id string) (Run, error) {
	// An id is a directory name. One that is a path would read a summary from
	// somewhere else.
	dir := filepath.Join(Dir(stateRoot), id)
	if filepath.Dir(dir) != filepath.Clean(Dir(stateRoot)) {
		return Run{}, fmt.Errorf("%w: %q", ErrNoRun, id)
	}
	b, err := os.ReadFile(filepath.Join(dir, summaryFile))
	if errors.Is(err, os.ErrNotExist) {
		return Run{}, fmt.Errorf("%w: %q", ErrNoRun, id)
	}
	if err != nil {
		return Run{}, fmt.Errorf("read run %s: %w", id, err)
	}
	var r Run
	if err := json.Unmarshal(b, &r); err != nil {
		return Run{}, fmt.Errorf("read run %s: %w", id, err)
	}
	r.Dir = dir
	return r, nil
}

// List returns every run, newest first. No runs yet is an empty list.
func List(stateRoot string) ([]Run, error) {
	entries, err := os.ReadDir(Dir(stateRoot))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	var runs []Run
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := Load(stateRoot, e.Name())
		if err != nil {
			// A directory with an unreadable summary is still a run someone
			// may want to look inside; listing it by name says where it is.
			r = Run{ID: e.Name(), Dir: filepath.Join(Dir(stateRoot), e.Name())}
		}
		runs = append(runs, r)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID > runs[j].ID })
	return runs, nil
}
