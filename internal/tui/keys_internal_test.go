package tui

import (
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
)

// config.Load refuses an action on a key the surface takes for itself, and it
// can know those keys only from its own list. Every chord the surface claims
// that is not typed text is on that list, and nothing else is.
func TestConfigSurfaceKeysAreTheSurfaceKeys(t *testing.T) {
	var names []string
	for _, b := range newKeyMap(nil).own() {
		names = append(names, b.Keys()...)
	}
	for _, a := range barActions {
		names = append(names, a.key)
	}
	var claimed []core.Chord
	for _, name := range names {
		c, err := core.ParseChord(name)
		if err != nil {
			t.Fatalf("surface key %q: %v", name, err)
		}
		if !c.Typed() {
			claimed = append(claimed, c)
		}
	}
	slices.Sort(claimed)
	want := slices.Clone(config.SurfaceKeys)
	slices.Sort(want)
	if !slices.Equal(claimed, want) {
		t.Errorf("surface claims %v, config.SurfaceKeys = %v", claimed, want)
	}
}
