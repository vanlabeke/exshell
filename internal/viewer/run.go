package viewer

import (
	tea "charm.land/bubbletea/v2"

	"vanlabeke.dev/exshell/internal/table"
)

// Run sets up and runs the Bubble Tea program for bk starting on sheet
// active. The alternate screen and mouse settings are declared per-frame by
// Model.View (v2 has no WithAltScreen/WithMouseCellMotion ProgramOptions);
// the initial 0x0 size here is immediately replaced by the real terminal
// size, which Bubble Tea reports via a WindowSizeMsg sent once at startup.
func Run(bk table.Book, active string, opts Options) error {
	m := NewModel(bk, active, 0, 0, opts)
	p := tea.NewProgram(&m)
	_, err := p.Run()
	return err
}
