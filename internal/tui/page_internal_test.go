package tui

import "testing"

// A page of agents is the rows that fit on the pane in the direction it
// moves: page up from the last agent counts the rows above it, not the none
// below it.
func TestAPageOfAgentsCountsTheRowsInItsDirection(t *testing.T) {
	m := Model{focus: focusAgents}
	m.detail.Height = 6
	for i := range 20 {
		m.alines = append(m.alines, lineSpan{start: 2 * i, end: 2*i + 2})
	}

	m.acursor = 19
	if got := m.page(-1); got != 3 {
		t.Errorf("page up from the last agent = %d, want 3", got)
	}
	if got := m.page(1); got != 1 {
		t.Errorf("page down from the last agent = %d, want 1", got)
	}

	m.acursor = 0
	if got := m.page(1); got != 3 {
		t.Errorf("page down from the first agent = %d, want 3", got)
	}
	if got := m.page(-1); got != 1 {
		t.Errorf("page up from the first agent = %d, want 1", got)
	}
}
