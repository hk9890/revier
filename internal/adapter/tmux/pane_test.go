// Layer L1: the form of a tmux pane id.
package tmux_test

import (
	"testing"

	"github.com/hk9890/revier/internal/adapter/tmux"
)

// A kitty window id is a bare number and a herdr pane id names its workspace,
// so neither may pass for a tmux pane.
func TestIsPaneTakesOnlyATmuxPaneID(t *testing.T) {
	cases := map[string]bool{
		"%5":    true,
		"%123":  true,
		"5":     false,
		"w1:p2": false,
		"%":     false,
		"%-1":   false,
		"%5a":   false,
		"":      false,
	}
	for id, want := range cases {
		if got := tmux.IsPane(id); got != want {
			t.Errorf("IsPane(%q) = %v, want %v", id, got, want)
		}
	}
}
