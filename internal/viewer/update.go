package viewer

import (
	tea "charm.land/bubbletea/v2"
)

// Init sends no initial command; the first WindowSizeMsg (sent automatically
// by the Bubble Tea runtime) is what sizes the model for real.
func (m *Model) Init() tea.Cmd { return nil }

// Update is a pure function of (Model, Msg): every case below only reads and
// writes m's fields, so the whole model is testable with no terminal
// attached by constructing it with NewModel and calling Update directly.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.width < 0 {
			m.width = 0
		}
		if m.height < 0 {
			m.height = 0
		}
		m.adjustViewport()
		return m, nil
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

// handleKey dispatches one key press. The search prompt and the help
// overlay each capture the keyboard while active, ahead of the normal
// bindings table.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.searchActive {
		m.handleSearchInputKey(msg)
		return nil
	}

	if m.help {
		switch msg.String() {
		case "?", "esc":
			m.help = false
		case "q", "ctrl+c":
			return tea.Quit
		}
		return nil
	}

	switch msg.String() {
	case "up", "k":
		m.moveCursor(-1, 0)
	case "down", "j":
		m.moveCursor(1, 0)
	case "left", "h":
		m.moveCursor(0, -1)
	case "right", "l":
		m.moveCursor(0, 1)
	case "pgup", "ctrl+b":
		m.pageUp()
	case "pgdown", "ctrl+f":
		m.pageDown()
	case "g":
		m.setCursorRow(0)
	case "G":
		m.setCursorRow(m.nRows() - 1)
	case "home":
		m.setCursorCol(0)
	case "end":
		m.setCursorCol(m.nCols() - 1)
	case "/":
		m.searchActive = true
		m.searchInput = ""
		m.status = ""
	case "n":
		m.search(true)
	case "N":
		m.search(false)
	case "enter":
		m.inspect = !m.inspect
	case "tab":
		m.switchSheet(1)
	case "shift+tab":
		m.switchSheet(-1)
	case "?":
		m.help = true
	case "q", "ctrl+c":
		return tea.Quit
	}
	return nil
}

// handleSearchInputKey edits the search prompt. Esc cancels without moving
// the cursor; Enter commits the query and runs the first forward search;
// Backspace removes the last rune (not byte, for multi-byte input);
// everything else appends its printable text if it has any — which is what
// makes "q" and "j" plain text here instead of quit/down while the prompt
// is open, since they carry Key.Text but match none of the named cases.
func (m *Model) handleSearchInputKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "esc":
		m.searchActive = false
		m.searchInput = ""
		return
	case "enter":
		m.searchActive = false
		m.lastQuery = m.searchInput
		m.searchInput = ""
		m.search(true)
		return
	case "backspace":
		if m.searchInput != "" {
			r := []rune(m.searchInput)
			m.searchInput = string(r[:len(r)-1])
		}
		return
	}

	if text := msg.Key().Text; text != "" {
		m.searchInput += text
	}
}
