// Package state persists what revier learned at runtime rather than what the
// user declared: which project was focused last, and which instances were
// attached to a project by hand.
//
// It is separate from config because it is written by revier and owned by the
// machine, not edited by the user and owned by the repository.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hk9890/revier/pkg/revier"
)

// State is the whole persisted document.
type State struct {
	// Current is the project revier focused last. It is how a keybinding knows
	// which project `revier go editor` means: the focused window cannot answer
	// it, because when you press the key to come back, the editor is focused
	// and not the workspace.
	Current revier.ProjectName `json:"current,omitempty"`

	// Attached maps a project to instances bound to it by hand. Instance ids
	// do not survive an application restart, so these are validated against
	// live instances on read and dropped when stale.
	Attached map[revier.ProjectName][]revier.TargetRef `json:"attached,omitempty"`

	// Bound maps a project's targets to the instance each last landed on.
	// A key is stable across an application's title changes because of it:
	// the rule finds a window once, the binding finds it after. Pruned like
	// attachments.
	Bound map[revier.ProjectName]map[revier.TargetName]revier.TargetRef `json:"bound,omitempty"`

	// Launch is the last time revier launched a window without getting a ref
	// to it, so the window that appears shortly after can be bound to the
	// target, or attached to the project when an action launched it. The
	// process that launches may exit before the window appears; the TUI
	// reads this and finishes the job.
	Launch *Launch `json:"launch,omitempty"`
}

// Launch is one detached launch: which project, which target if any, when.
type Launch struct {
	Project revier.ProjectName `json:"project"`
	Target  revier.TargetName  `json:"target,omitempty"`
	At      time.Time          `json:"at"`
}

// Root reports the state directory, honouring REVIER_STATE_HOME and then
// XDG_STATE_HOME. The override is what lets a test and scripts/drive keep off
// the user's real state.
func Root() (string, error) {
	if r := os.Getenv("REVIER_STATE_HOME"); r != "" {
		return r, nil
	}
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "revier"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "revier"), nil
}

func path(root string) string { return filepath.Join(root, "state.json") }

// Load reads the state. A missing file is an empty state, not an error: the
// first run of revier has none.
func Load(root string) (*State, error) {
	b, err := os.ReadFile(path(root))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &State{Attached: map[revier.ProjectName][]revier.TargetRef{}}, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		// Corrupt state is discarded rather than fatal. It holds only a
		// convenience - the last project and some attachments - and refusing
		// to start over it would be worse than losing it.
		return &State{Attached: map[revier.ProjectName][]revier.TargetRef{}}, nil
	}
	if s.Attached == nil {
		s.Attached = map[revier.ProjectName][]revier.TargetRef{}
	}
	return &s, nil
}

// Update applies a change to the state on disk under a lock and saves it when
// apply reports a change. Every process that writes state - the TUI, and each
// keypress's CLI process - reads, changes and saves it, so without the lock a
// launch one records between another's read and save is overwritten, and the
// next press opens a duplicate window. apply runs while the lock is held, so it
// does no I/O.
func Update(root string, apply func(s *State) bool) (*State, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(root, "state.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open state lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return nil, fmt.Errorf("lock state: %w", err)
	}
	s, err := Load(root)
	if err != nil {
		return nil, err
	}
	if apply(s) {
		if err := s.Save(root); err != nil {
			return s, err
		}
	}
	return s, nil
}

// Save writes the state atomically, so a crash mid-write cannot leave a
// truncated file that the next run has to discard.
func (s *State) Save(root string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	tmp, err := os.CreateTemp(root, "state-*.json")
	if err != nil {
		return fmt.Errorf("create temp state: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state: %w", err)
	}
	return os.Rename(tmp.Name(), path(root))
}

// Pending reports whether l, the launch on record if any, is of the project's
// target and younger than within: a press that finds one must not launch the
// target again.
func (l *Launch) Pending(p revier.ProjectName, t revier.TargetName, within time.Duration) bool {
	return l != nil && l.Project == p && l.Target == t && time.Since(l.At) < within
}

// Launched records a launch whose window has not appeared, and makes its
// project the current one. An action's launch has no target.
func (s *State) Launched(p revier.ProjectName, t revier.TargetName, at time.Time) {
	s.Current = p
	s.Launch = &Launch{Project: p, Target: t, At: at}
}

// Landed records the instance a target landed on and makes its project the
// current one. Every binding of a target consumes its launch.
func (s *State) Landed(p revier.ProjectName, t revier.TargetName, ref revier.TargetRef) {
	s.Current = p
	s.Bind(p, t, ref)
	if s.Launch != nil && s.Launch.Project == p && s.Launch.Target == t {
		s.Launch = nil
	}
}

// Claim settles the pending launch with the window that appeared for it: bound
// to the launched target, or attached to the project when an action launched
// it.
func (s *State) Claim(t revier.TargetName, ref revier.TargetRef) {
	if t != "" {
		s.Bind(s.Launch.Project, t, ref)
	} else {
		s.Attach(s.Launch.Project, ref)
	}
	s.Launch = nil
}

// Attach binds an instance to a project, ignoring a duplicate.
func (s *State) Attach(p revier.ProjectName, ref revier.TargetRef) {
	if s.Attached == nil {
		s.Attached = map[revier.ProjectName][]revier.TargetRef{}
	}
	for _, existing := range s.Attached[p] {
		if existing.Host == ref.Host && existing.ID == ref.ID {
			return
		}
	}
	s.Attached[p] = append(s.Attached[p], ref)
}

// Rename moves everything recorded under a project to its new name.
func (s *State) Rename(from, to revier.ProjectName) {
	if s.Current == from {
		s.Current = to
	}
	if refs, ok := s.Attached[from]; ok {
		delete(s.Attached, from)
		s.Attached[to] = refs
	}
	if targets, ok := s.Bound[from]; ok {
		delete(s.Bound, from)
		s.Bound[to] = targets
	}
	if s.Launch != nil && s.Launch.Project == from {
		s.Launch.Project = to
	}
}

// Bind records where a target last landed.
func (s *State) Bind(p revier.ProjectName, t revier.TargetName, ref revier.TargetRef) {
	if s.Bound == nil {
		s.Bound = map[revier.ProjectName]map[revier.TargetName]revier.TargetRef{}
	}
	if s.Bound[p] == nil {
		s.Bound[p] = map[revier.TargetName]revier.TargetRef{}
	}
	s.Bound[p][t] = ref
}

// Prune drops attachments and bindings whose instances are gone and reports
// whether any were. hosts are the hosts a survey listed and instances what it
// found. A ref on any other host is kept: a survey that did not ask its host -
// a TUI started over SSH, a window host that did not probe - cannot tell a
// closed window from one it never looked for.
//
// before is the state the survey started from. A ref it does not hold was
// written while the survey was listing, for a window that may have opened
// after the listing was taken, so it is kept for the next survey to judge.
func (s *State) Prune(hosts []string, instances []revier.Instance, before *State) bool {
	asked := map[string]bool{}
	for _, h := range hosts {
		asked[h] = true
	}
	live := map[string]bool{}
	for _, inst := range instances {
		live[key(inst.Ref)] = true
	}
	known := before.refs()
	gone := func(ref revier.TargetRef) bool { return asked[ref.Host] && known[key(ref)] && !live[key(ref)] }

	changed := false
	for project, targets := range s.Bound {
		for t, ref := range targets {
			if gone(ref) {
				delete(targets, t)
				changed = true
			}
		}
		if len(targets) == 0 {
			delete(s.Bound, project)
		}
	}
	for project, refs := range s.Attached {
		kept := refs[:0]
		for _, ref := range refs {
			if !gone(ref) {
				kept = append(kept, ref)
			}
		}
		if len(kept) != len(refs) {
			changed = true
		}
		if len(kept) == 0 {
			delete(s.Attached, project)
			continue
		}
		s.Attached[project] = kept
	}
	return changed
}

// refs is every attached and bound ref, by key. A nil state holds none.
func (s *State) refs() map[string]bool {
	out := map[string]bool{}
	if s == nil {
		return out
	}
	for _, targets := range s.Bound {
		for _, ref := range targets {
			out[key(ref)] = true
		}
	}
	for _, refs := range s.Attached {
		for _, ref := range refs {
			out[key(ref)] = true
		}
	}
	return out
}

// key is the identity used to compare refs across a save and load.
func key(ref revier.TargetRef) string { return ref.Host + "\x00" + ref.ID }
