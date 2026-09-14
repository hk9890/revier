package tui

// What the pointer is on. The surface lights it, a step below the selection,
// so a row or a button says it can be clicked before it is.
//
// Two things can be lit at once: the row the keys act on, and the row the
// pointer is over. They must not look alike, which is why hover has its own
// background and never the selection's (decisions.md D50).
type hoverKind int

const (
	hoverNone   hoverKind = iota
	hoverBar              // a button on the action bar
	hoverRow              // a row of the list
	hoverTarget           // a target row in the pane
	hoverAgent            // an agent row in the pane
	hoverField            // a section's query field; index is its focus
)

type hovered struct {
	kind  hoverKind
	index int
}

// pointerCell is the terminal cell the pointer is on.
type pointerCell struct{ x, y int }

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
		return m.paneAt(y)
	}
	if mr, _ := m.margins(); m.dialog == dialogNone && y == mr+queryRow {
		return hovered{kind: hoverField, index: int(focusList)}
	}
	if i, ok := m.rowAt(x, y); ok {
		return hovered{kind: hoverRow, index: i}
	}
	return hovered{}
}

// paneLine is the line of the pane's view a terminal row is on. Beside the
// list the pane starts on the query line; in the list's place, where the list
// starts.
func (m Model) paneLine(y int) (int, bool) {
	if m.paneWidth() == 0 {
		return m.bodyLine(y)
	}
	mr, _ := m.margins()
	top := mr + queryRow
	if y < top || y >= top+m.detail.Height {
		return 0, false
	}
	return y - top, true
}

// paneAt is what of the pane is on a terminal row: a section's query field, a
// target row or an agent row, if one is there.
func (m Model) paneAt(y int) hovered {
	line, ok := m.paneLine(y)
	if _, selected := m.selected(); m.dialog != dialogNone || !ok || !selected {
		return hovered{}
	}
	line += m.detail.YOffset
	switch line {
	case m.tfield:
		return hovered{kind: hoverField, index: int(focusTargets)}
	case m.afield:
		return hovered{kind: hoverField, index: int(focusAgents)}
	}
	for i, at := range m.tlines {
		if at == line {
			return hovered{kind: hoverTarget, index: i}
		}
	}
	for i, span := range m.alines {
		if line >= span.start && line < span.end {
			return hovered{kind: hoverAgent, index: i}
		}
	}
	return hovered{}
}
