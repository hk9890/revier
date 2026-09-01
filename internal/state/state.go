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
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state: %w", err)
	}
	return os.Rename(tmp.Name(), path(root))
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

// Prune drops attachments whose instances are gone. live holds the refs a
// survey found, keyed "host\x00id".
func (s *State) Prune(live map[string]bool) {
	for project, refs := range s.Attached {
		kept := refs[:0]
		for _, ref := range refs {
			if live[Key(ref)] {
				kept = append(kept, ref)
			}
		}
		if len(kept) == 0 {
			delete(s.Attached, project)
			continue
		}
		s.Attached[project] = kept
	}
}

// Key is the identity used to compare refs across a save and load.
func Key(ref revier.TargetRef) string { return ref.Host + "\x00" + ref.ID }
