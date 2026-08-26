package viewer

import (
	"io"

	tea "charm.land/bubbletea/v2"

	"vanlabeke.dev/exshell/internal/table"
)

// Run sets up and runs the Bubble Tea program for bk starting on sheet
// active, writing the TUI's escape stream to stdout — never to the
// process's real os.Stdout directly, matching the contract every other exit
// from internal/cli honors (see cli.go's Run doc), so a caller-supplied
// stdout (a test's buffer, or a real terminal file the caller resolved
// itself) is always where the program actually draws. The alternate screen
// and mouse settings are declared per-frame by Model.View (v2 has no
// WithAltScreen/WithMouseCellMotion ProgramOptions); the initial 0x0 size
// here is immediately replaced by the real terminal size, which Bubble Tea
// reports via a WindowSizeMsg sent once at startup.
//
// Latent caveat, not reachable from cli.go today (ModeViewer is only chosen
// when stdout is a real terminal): stdout being something other than an
// *os.File — a plain io.Writer a future caller supplies directly — makes
// Bubble Tea unable to query or receive SIGWINCH for its size, silently
// degrading the program to 0x0 while stdin is still put into raw mode.
func Run(bk table.Book, active string, stdout io.Writer, opts Options) error {
	m := NewModel(bk, active, 0, 0, opts)
	p := tea.NewProgram(&m, tea.WithOutput(stdout))
	_, err := p.Run()
	return err
}
