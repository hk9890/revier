package tui

import tea "charm.land/bubbletea/v2"

// bare is a model built field by field, as a test that needs only a few of
// them builds it, with the screens New would give it: Model holds them by
// pointer.
func bare(m Model) Model {
	m.link, m.sessions, m.config, m.proj, m.agents, m.create = &linkScreen{}, &sessionsScreen{}, &configScreen{}, &projectScreen{}, &agentList{}, &createScreen{}
	return m
}

// fold is a press as it reaches the screens: folded as Update folds it.
func fold(k tea.KeyPressMsg) keyPress {
	folded, _ := legacyKey(k)
	return pressedKey(folded)
}

func texts(segs []segment) []string {
	var out []string
	for _, s := range segs {
		out = append(out, s.Text)
	}
	return out
}
