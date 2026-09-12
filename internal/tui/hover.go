package tui

// What the pointer is on. The surface lights it, a step below the selection,
// so a row or a button says it can be clicked before it is.
//
// Two things can be lit at once: the row the keys act on, and the row the
// pointer is over. They must not look alike, which is why hover has its own
// background and never the selection's (decisions.md D36).
type hoverKind int

const (
	hoverNone   hoverKind = iota
	hoverBar              // a button on the action bar
	hoverRow              // a row of the list
	hoverTarget           // a target row in the pane
)

type hovered struct {
	kind  hoverKind
	index int
}

func (h hovered) is(k hoverKind, index int) bool {
	return h.kind == k && h.index == index
}

// hoverAt is what a terminal cell is over. The bar is asked first: it is the
// one thing above the rows, and a button is small.
func (m Model) hoverAt(x, y int) hovered {
	if i := m.barAt(x, y); i >= 0 {
		return hovered{kind: hoverBar, index: i}
	}
	if m.overPane(x) {
		if i, ok := m.targetAt(y); ok {
			return hovered{kind: hoverTarget, index: i}
		}
		return hovered{}
	}
	if i, ok := m.rowAt(x, y); ok {
		return hovered{kind: hoverRow, index: i}
	}
	return hovered{}
}

// targetAt is the pane's target row on a terminal row, if one is there.
func (m Model) targetAt(y int) (int, bool) {
	if m.dialog != dialogNone {
		return 0, false
	}
	line, ok := m.bodyLine(y)
	if !ok {
		return 0, false
	}
	for i, at := range m.tlines {
		if at == line+m.detail.YOffset {
			return i, true
		}
	}
	return 0, false
}
