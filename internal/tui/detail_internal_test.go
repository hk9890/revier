package tui

import (
	"slices"
	"testing"
)

// The pane appears at the least width that holds both the list and the pane,
// and not one column below it.
func TestThePaneAppearsAtTheLeastWidthThatHoldsItBesideTheList(t *testing.T) {
	least := minListWidth + minPaneWidth
	for _, c := range []struct{ inner, pane int }{{least - 1, 0}, {least, minPaneWidth}} {
		m := bare(Model{width: c.inner + 2*marginCols, height: 30})
		if w, _ := m.inner(); w != c.inner {
			t.Fatalf("inner width = %d, want %d", w, c.inner)
		}
		if got := m.paneWidth(); got != c.pane {
			t.Errorf("pane width at %d inner columns = %d, want %d", c.inner, got, c.pane)
		}
	}
}

// A match at the first byte of the next segment is that segment's, not this
// one's: no letter is lit past the end of the part that holds it.
func TestAMatchAtASegmentsEndBelongsToTheNextSegment(t *testing.T) {
	matches := []int{3, 4, 7, 8}
	if got := within(matches, 4, 4); !slices.Equal(got, []int{0, 3}) {
		t.Errorf("within(%v, 4, 4) = %v, want [0 3]", matches, got)
	}
}
