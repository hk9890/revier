// Package session persists the set of projects that were open at a moment, so
// the same set can be opened again after a reboot.
//
// It holds names and nothing else. The host that realized a target, the
// instance it landed on, and the argv it was launched with are all derived
// again at restore from the project file as it reads then, so a project file
// edited between the two wins over the recording. The exception is what an
// agent was doing - its conversation and the directory it worked in - which no
// project file can carry, because neither existed when the file was written.
//
// It is separate from internal/state because the two have different
// lifecycles. state.json is one live document, pruned on read against the
// instances that exist now; these are many durable artifacts that must survive
// exactly the event - a reboot - that makes every ref in state.json stale.
package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/hk9890/revier/internal/fsutil"
	"github.com/hk9890/revier/pkg/revier"
)

// ErrNoSession is returned when no stored session answers the request: an
// empty store, or a name and id that name none. An empty store asked for the
// newest session is a normal outcome - the first restore on a machine that
// never saved - and the caller says so rather than reporting a failure. A
// name that names nothing is a typo, and is one.
var ErrNoSession = errors.New("no stored session")

// ErrNoWholeSession is returned for the newest session when sessions are
// stored but none stands for the whole set: each was saved as a project
// closed. It wraps ErrNoSession, so a caller that only tells an empty store
// from a failure still does (decisions.md D149).
var ErrNoWholeSession = fmt.Errorf("%w of every open project", ErrNoSession)

// Session is one recorded set of open projects.
type Session struct {
	// ID is the moment it was saved, and the name of its file. A timestamp
	// rather than a uuid: these never leave the machine and never merge, so
	// uniqueness across machines buys nothing, while sorting and being
	// typeable are used on every restore.
	ID string `toml:"id" json:"id"`

	// Name is an optional label. Several sessions may carry one; the newest
	// wins when a restore asks for it by name.
	Name string `toml:"name,omitempty" json:"name,omitempty"`

	// Reason is why it was saved: `revier session list` shows it beside the
	// Name, and the TUI list in place of a missing one. It is not folded into
	// Name, because a restore resolves a ref by Name.
	// A file written before the field existed has none.
	Reason Reason `toml:"reason,omitempty" json:"reason,omitempty"`

	At time.Time `toml:"at" json:"at"`

	// Current is the project that was focused. Restore ends on it, so a
	// restored desktop lands where the saved one was left.
	Current revier.ProjectName `toml:"current,omitempty" json:"current,omitempty"`

	Projects []Project `toml:"project" json:"projects"`
}

// Reason is why a session was saved.
type Reason string

const (
	// ReasonShutdown is the save a full shutdown makes before it closes.
	ReasonShutdown Reason = "shutdown"
	// ReasonProjectClosed is the save made before one project is closed.
	// It is not what a plain restore opens: a run of closes leaves the
	// smallest set as the newest save (decisions.md D149).
	ReasonProjectClosed Reason = "project-closed"
	// ReasonSaved is a save asked for by hand.
	ReasonSaved Reason = "saved"
	// ReasonLastSeen is what a surface last saw, saved when it ended without
	// a shutdown.
	ReasonLastSeen Reason = "last-seen"
)

// Whole reports whether a save of this reason stands for the whole set that
// was open, so that a plain restore may open it (decisions.md D149). A file
// with no reason predates the field and cannot be told apart, so it counts;
// a reason this build does not know does not.
func (r Reason) Whole() bool {
	switch r {
	case ReasonShutdown, ReasonSaved, ReasonLastSeen, "":
		return true
	default:
		return false
	}
}

// Text is the reason as a list shows it. A file with no reason, or one
// this build does not know, reads "unknown" rather than a guess.
func (r Reason) Text() string {
	switch r {
	case ReasonShutdown:
		return "shutdown"
	case ReasonProjectClosed:
		return "project closed"
	case ReasonSaved:
		return "saved by hand"
	case ReasonLastSeen:
		return "last seen"
	default:
		return "unknown"
	}
}

// Project is one project and the targets that were live in it.
type Project struct {
	Name    revier.ProjectName `toml:"name" json:"name"`
	Targets []Target           `toml:"target" json:"targets"`
}

// Target is a table rather than a bare name so a key can be added to it - as
// Agents was - without breaking a format someone already has files in.
type Target struct {
	Name revier.TargetName `toml:"name" json:"name"`

	// Agents are the agents the target's instance held, in the order the
	// runtime listed them. The order is their identity: a restore gives the
	// first to the first agent panel the project declares, and so on, and
	// opens the rest beside the layout (decisions.md D146).
	Agents []Agent `toml:"agent,omitempty" json:"agents,omitempty"`
}

// Agent is one agent: the harness its probe named, and what that probe could
// say about its conversation. An agent with neither holds its place in the
// order, so the agents after it keep theirs. Name is the named agent it ran,
// which a restore opens again and no other program in its place; it is empty
// for the project's default agent, and in a file written before the field
// existed (decisions.md D146).
type Agent struct {
	Harness string           `toml:"harness" json:"harness"`
	Session revier.SessionID `toml:"session,omitempty" json:"session,omitempty"`
	Dir     string           `toml:"dir,omitempty" json:"dir,omitempty"`
	Name    string           `toml:"name,omitempty" json:"name,omitempty"`
}

// Dir is where sessions live under the state root.
func Dir(stateRoot string) string { return filepath.Join(stateRoot, "sessions") }

// NewID is the identity of a session saved at at: sortable, readable, and a
// legal filename on every filesystem revier runs on.
func NewID(at time.Time) string { return at.Format("2006-01-02T15-04-05") }

// Save writes the session, and returns it as stored together with the file it
// wrote. The id on s is ignored: Save assigns one from At, suffixing it when a
// session of that second already exists, so two saves in one second do not
// silently become one. It is returned rather than only written because the id
// is what the caller has to print and the caller cannot predict the suffix.
func Save(stateRoot string, s Session) (Session, string, error) {
	dir := Dir(stateRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Session{}, "", fmt.Errorf("create session directory: %w", err)
	}
	id, path := free(dir, s.At)
	s.ID = id
	if err := write(path, s); err != nil {
		return Session{}, "", err
	}
	return s, path, nil
}

// free is the id of a session saved at at that no file in dir has yet, and
// its file.
func free(dir string, at time.Time) (string, string) {
	base := NewID(at)
	id, path := base, filepath.Join(dir, base+".toml")
	for n := 2; exists(path); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
		path = filepath.Join(dir, id+".toml")
	}
	return id, path
}

func write(path string, s Session) error {
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(s); err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	// Whole or not at all: Rename writes every stored session through here,
	// and a session cut short by a crash is one List drops without a word.
	if err := fsutil.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Rename moves what every stored session records under a project to its new
// name, in place: a project renamed on the surface (decisions.md D80) is
// still the one a session saved before the rename opens again. A session
// that does not name the project is left as it is. What a surface last saw
// is renamed too, so a rename made while no surface runs is not lost when
// that file becomes a saved session.
func Rename(stateRoot string, from, to revier.ProjectName) error {
	all, err := List(stateRoot)
	if err != nil {
		return err
	}
	for _, s := range all {
		if renamed(&s, from, to) {
			if err := write(filepath.Join(Dir(stateRoot), s.ID+".toml"), s); err != nil {
				return err
			}
		}
	}
	seen, err := LoadSeen(stateRoot)
	switch {
	case errors.Is(err, ErrNoSession):
		return nil
	case err != nil:
		return err
	}
	if renamed(&seen, from, to) {
		return write(SeenPath(stateRoot), seen)
	}
	return nil
}

// renamed moves what s records under from to to, and reports whether s named
// from at all.
func renamed(s *Session, from, to revier.ProjectName) bool {
	changed := false
	if s.Current == from {
		s.Current, changed = to, true
	}
	for i := range s.Projects {
		if s.Projects[i].Name == from {
			s.Projects[i].Name, changed = to, true
		}
	}
	return changed
}

// SeenPath is the file that holds what a running surface last saw. It is
// beside the sessions directory and not in it, so List and a plain restore
// never take it for a save: it becomes one only after an end with no
// shutdown, by PromoteSeen (decisions.md D150).
func SeenPath(stateRoot string) string { return filepath.Join(stateRoot, "seen.toml") }

// SaveSeen writes s as what the surface last saw, over the file it wrote
// before, whole or not at all. It carries no id, since it is no stored
// session, and the reason it is saved under when it becomes one.
func SaveSeen(stateRoot string, s Session) error {
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	s.ID, s.Reason = "", ReasonLastSeen
	return write(SeenPath(stateRoot), s)
}

// LoadSeen reads what a surface last saw, or ErrNoSession when no surface
// wrote it or it was taken since.
func LoadSeen(stateRoot string) (Session, error) {
	var s Session
	if _, err := toml.DecodeFile(SeenPath(stateRoot), &s); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Session{}, ErrNoSession
		}
		return Session{}, fmt.Errorf("read %s: %w", SeenPath(stateRoot), err)
	}
	return s, nil
}

// PromoteSeen moves what a surface last saw into the stored sessions, as a
// session of its own moment, and returns it as stored together with the file
// it is now. A move is one step, so of two surfaces that start at once one
// takes the file and the other gets ErrNoSession: what it would offer is
// offered already.
func PromoteSeen(stateRoot string) (Session, string, error) {
	s, err := LoadSeen(stateRoot)
	if err != nil {
		return Session{}, "", err
	}
	dir := Dir(stateRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Session{}, "", fmt.Errorf("create session directory: %w", err)
	}
	id, path := free(dir, s.At)
	if err := os.Rename(SeenPath(stateRoot), path); err != nil {
		return Session{}, "", taken(err)
	}
	s.ID = id
	return s, path, nil
}

// DropSeen removes what a surface last saw, once a stored session holds the
// same: the file is taken as PromoteSeen takes it, so it is offered once,
// and the one that finds it gone gets ErrNoSession.
func DropSeen(stateRoot string) error {
	return taken(os.Remove(SeenPath(stateRoot)))
}

// taken is the error of a move or a removal of what was last seen: none, or
// ErrNoSession when another process took the file first.
func taken(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrNotExist):
		return ErrNoSession
	default:
		return fmt.Errorf("take what was last seen: %w", err)
	}
}

// Load reads one session. An empty ref is what a restore after a reboot
// means: the session Newest picks, or ErrNoWholeSession when none stands for
// the whole set (decisions.md D149). Otherwise ref is an id, and failing that
// a name, whose newest holder is taken, whatever its reason.
func Load(stateRoot, ref string) (Session, error) {
	all, err := List(stateRoot)
	if err != nil {
		return Session{}, err
	}
	if len(all) == 0 {
		return Session{}, ErrNoSession
	}
	if ref == "" {
		s, ok := Newest(all)
		if !ok {
			return Session{}, ErrNoWholeSession
		}
		return s, nil
	}
	for _, s := range all {
		if s.ID == ref {
			return s, nil
		}
	}
	// List is newest first, so the first name match is the newest holder.
	for _, s := range all {
		if s.Name == ref {
			return s, nil
		}
	}
	return Session{}, fmt.Errorf("%w named %q", ErrNoSession, ref)
}

// Newest is the first session of a newest-first list whose reason stands for
// the whole set: the one a plain restore opens (decisions.md D149).
func Newest(all []Session) (Session, bool) {
	for _, s := range all {
		if s.Reason.Whole() {
			return s, true
		}
	}
	return Session{}, false
}

// List reads every stored session, newest first. A file that does not parse is
// skipped rather than fatal: one hand-edited session must not hide the rest.
func List(stateRoot string) ([]Session, error) {
	entries, err := os.ReadDir(Dir(stateRoot))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read session directory: %w", err)
	}
	var out []Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		var s Session
		if _, err := toml.DecodeFile(filepath.Join(Dir(stateRoot), e.Name()), &s); err != nil {
			continue
		}
		if s.ID == "" {
			s.ID = strings.TrimSuffix(e.Name(), ".toml")
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, nil
}

// Agents counts every agent recorded, with a conversation or without one.
func (s Session) Agents() int {
	n := 0
	for _, p := range s.Projects {
		for _, t := range p.Targets {
			n += len(t.Agents)
		}
	}
	return n
}

// Conversations counts the agents recorded with a conversation to resume.
func (s Session) Conversations() int {
	n := 0
	for _, p := range s.Projects {
		for _, t := range p.Targets {
			for _, a := range t.Agents {
				if a.Session != "" {
					n++
				}
			}
		}
	}
	return n
}

// Targets counts the targets across every project, the number a save reports.
func (s Session) Targets() int {
	n := 0
	for _, p := range s.Projects {
		n += len(p.Targets)
	}
	return n
}

// SameAs reports whether two sessions hold the same projects, targets and
// agents: what a restore of either would open. The id, the name, the reason,
// the moment and the project ended on are not what is open, and the order of the
// projects is the survey's; the order of the agents is their identity.
func (s Session) SameAs(o Session) bool {
	byName := func(ps []Project) []Project {
		out := slices.Clone(ps)
		slices.SortStableFunc(out, func(a, b Project) int { return strings.Compare(string(a.Name), string(b.Name)) })
		return out
	}
	return slices.EqualFunc(byName(s.Projects), byName(o.Projects), func(a, b Project) bool {
		return a.Name == b.Name && slices.EqualFunc(a.Targets, b.Targets, func(x, y Target) bool {
			return x.Name == y.Name && slices.Equal(x.Agents, y.Agents)
		})
	})
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
