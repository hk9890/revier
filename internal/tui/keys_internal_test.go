package tui

import (
	"io"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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

// keyRecorder is a program that keeps the first key it is sent and quits.
type keyRecorder struct{ got *[]tea.KeyMsg }

func (r keyRecorder) Init() tea.Cmd { return nil }
func (r keyRecorder) View() string  { return "" }
func (r keyRecorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		*r.got = append(*r.got, k)
		return r, tea.Quit
	}
	return r, nil
}

// The desktop takes the key that opens the popup before the popup's terminal
// sees it, so `revier popup` types core.SwitchKey into a popup that is focused
// already. Those bytes, read by bubbletea's own input reader as a terminal's
// are, are the press the surface switches its list on.
func TestTheKeyTypedIntoTheFocusedPopupIsTheSwitch(t *testing.T) {
	var got []tea.KeyMsg
	p := tea.NewProgram(keyRecorder{&got}, tea.WithInput(strings.NewReader(core.SwitchKey)), tea.WithOutput(io.Discard))
	if _, err := p.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 1 || !switches(got[0]) {
		t.Errorf("read %q, want the one press that switches the list", got)
	}
}
