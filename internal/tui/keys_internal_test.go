package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
)

// config.Load refuses an action on a key the surface takes for itself, and it
// can know those keys only from its own list. Every ctrl or alt chord the
// surface claims is on that list, and nothing else is.
func TestConfigSurfaceKeysAreTheSurfaceKeys(t *testing.T) {
	k := newKeyMap(nil)
	var names []string
	for _, b := range []key.Binding{k.Up, k.Down, k.Enter, k.Targets, k.Back, k.Quit, k.Edit, k.Delete} {
		names = append(names, b.Keys()...)
	}
	for _, a := range barActions {
		names = append(names, a.key)
	}
	var claimed []core.Chord
	for _, name := range names {
		if !strings.HasPrefix(name, "ctrl+") && !strings.HasPrefix(name, "alt+") {
			continue
		}
		c, err := core.ParseChord(name)
		if err != nil {
			t.Fatalf("surface key %q: %v", name, err)
		}
		claimed = append(claimed, c)
	}
	slices.Sort(claimed)
	want := slices.Clone(config.SurfaceKeys)
	slices.Sort(want)
	if !slices.Equal(claimed, want) {
		t.Errorf("surface claims %v, config.SurfaceKeys = %v", claimed, want)
	}
}
