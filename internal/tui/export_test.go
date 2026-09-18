package tui

import tea "github.com/charmbracelet/bubbletea"

// LookupStart exposes the command Init runs for the starting project, so a
// test delivers its answer at a moment of its choosing.
func (m Model) LookupStart() tea.Cmd { return m.lookupStart() }

// Spin is one tick of the working spinner.
func Spin() tea.Msg { return spinMsg{} }
