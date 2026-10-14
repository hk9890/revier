package tui

// bare is a model built field by field, as a test that needs only a few of
// them builds it, with the screens New would give it: Model holds them by
// pointer.
func bare(m Model) Model {
	m.link, m.sessions, m.config, m.proj, m.agents = &linkScreen{}, &sessionsScreen{}, &configScreen{}, &projectScreen{}, &agentList{}
	return m
}
