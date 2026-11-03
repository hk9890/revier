package kitty

import "testing"

// Two Opens or OpenTabs never share a mark: a failed one would close the
// other's windows.
func TestEveryOpenGetsAMarkOfItsOwn(t *testing.T) {
	h := &Host{}
	if a, b := h.newMark(), h.newMark(); a == "" || a == b {
		t.Errorf("marks = %q and %q, want two that differ", a, b)
	}
}
