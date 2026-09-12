// Package session persists the set of projects that were open at a moment, so
// the same set can be opened again after a reboot.
//
// It holds names and nothing else. The host that realized a target, the
// instance it landed on, and the argv it was launched with are all derived
// again at restore from the project file as it reads then, so a project file
// edited between the two wins over the recording. The one exception is the
// agent session id, which no project file can carry because it did not exist
// when the file was written.
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
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/hk9890/revier/pkg/revier"
)

// ErrNoSession is returned when no stored session answers the request: an
// empty store, or a name and id that name none. It is a normal outcome - the
// first restore on a machine that never saved - and the caller says so rather
// than reporting a failure.
var ErrNoSession = errors.New("no stored session")

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

	At time.Time `toml:"at" json:"at"`

	// Current is the project that was focused. Restore ends on it, so a
	// restored desktop lands where the saved one was left.
	Current revier.ProjectName `toml:"current,omitempty" json:"current,omitempty"`

	Projects []Project `toml:"project" json:"projects"`
}

// Project is one project and the targets that were live in it.
type Project struct {
	Name    revier.ProjectName `toml:"name" json:"name"`
	Targets []Target           `toml:"target" json:"targets"`
}

// Target is a table rather than a bare name so a key can be added to it - as
// Panels was - without breaking a format someone already has files in.
type Target struct {
	Name revier.TargetName `toml:"name" json:"name"`

	// Panels are the agent conversations the target held, for the probes that
	// can name one. Index is the panel's position among all the target's
	// panels, the position of its spec in the project file, because a live
	// panel's title is the agent's to change and is no identity at all.
	Panels []Panel `toml:"panel,omitempty" json:"panels,omitempty"`
}

// Panel is one agent conversation: which panel of the target, which probe
// named it, and the probe's own word for the conversation.
type Panel struct {
	Index   int              `toml:"index" json:"index"`
	Harness string           `toml:"harness" json:"harness"`
	Session revier.SessionID `toml:"session" json:"session"`
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
	base := NewID(s.At)
	id, path := base, filepath.Join(dir, base+".toml")
	for n := 2; exists(path); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
		path = filepath.Join(dir, id+".toml")
	}
	s.ID = id

	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(s); err != nil {
		return Session{}, "", fmt.Errorf("encode session: %w", err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return Session{}, "", fmt.Errorf("write %s: %w", path, err)
	}
	return s, path, nil
}

// Load reads one session. An empty ref is the newest stored session, which is
// what a restore after a reboot means. Otherwise ref is an id, and failing
// that a name, whose newest holder is taken.
func Load(stateRoot, ref string) (Session, error) {
	all, err := List(stateRoot)
	if err != nil {
		return Session{}, err
	}
	if len(all) == 0 {
		return Session{}, ErrNoSession
	}
	if ref == "" {
		return all[0], nil
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

// Targets counts the targets across every project, the number a save reports.
func (s Session) Targets() int {
	n := 0
	for _, p := range s.Projects {
		n += len(p.Targets)
	}
	return n
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
