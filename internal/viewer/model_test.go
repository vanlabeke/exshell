package viewer

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// press builds a KeyPressMsg for a printable rune key with no modifiers,
// which is enough for the letter/digit keys exercised in these tests.
func press(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Text: string(r), Code: r}
}

// namedKey builds a KeyPressMsg for a special key by rune code, with no
// Text, matching how the real terminal driver reports them.
func namedKey(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

func sendKey(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	_, cmd := m.Update(msg)
	return cmd
}

func TestNewModel_InitialState(t *testing.T) {
	bk := singleBook(5, 3, nil)
	m := NewModel(bk, "sheet1", 40, 10, Options{})

	if m.cursorRow != 0 || m.cursorCol != 0 {
		t.Fatalf("cursor = (%d,%d), want (0,0)", m.cursorRow, m.cursorCol)
	}
	if m.rowOff != 0 || m.colOff != 0 {
		t.Fatalf("offsets = (%d,%d), want (0,0)", m.rowOff, m.colOff)
	}
	if m.active != "sheet1" {
		t.Fatalf("active = %q, want sheet1", m.active)
	}
	if m.nRows() != 5 || m.nCols() != 3 {
		t.Fatalf("dims = (%d,%d), want (5,3)", m.nRows(), m.nCols())
	}
}

func TestCursorMovement_FourDirectionsWithClamping(t *testing.T) {
	bk := singleBook(5, 3, nil)
	m := NewModel(bk, "sheet1", 80, 20, Options{})

	sendKey(&m, press('j'))
	sendKey(&m, press('j'))
	if m.cursorRow != 2 {
		t.Fatalf("after 2 down: cursorRow = %d, want 2", m.cursorRow)
	}

	for i := 0; i < 10; i++ {
		sendKey(&m, namedKey(tea.KeyUp, 0))
	}
	if m.cursorRow != 0 {
		t.Fatalf("clamp at top: cursorRow = %d, want 0", m.cursorRow)
	}

	for i := 0; i < 10; i++ {
		sendKey(&m, namedKey(tea.KeyDown, 0))
	}
	if m.cursorRow != 4 {
		t.Fatalf("clamp at bottom: cursorRow = %d, want 4 (nRows-1)", m.cursorRow)
	}

	for i := 0; i < 10; i++ {
		sendKey(&m, namedKey(tea.KeyRight, 0))
	}
	if m.cursorCol != 2 {
		t.Fatalf("clamp at right: cursorCol = %d, want 2 (nCols-1)", m.cursorCol)
	}

	for i := 0; i < 10; i++ {
		sendKey(&m, namedKey(tea.KeyLeft, 0))
	}
	if m.cursorCol != 0 {
		t.Fatalf("clamp at left: cursorCol = %d, want 0", m.cursorCol)
	}
}

func TestCursorMovement_HJKL(t *testing.T) {
	bk := singleBook(5, 5, nil)
	m := NewModel(bk, "sheet1", 80, 20, Options{})

	sendKey(&m, press('l'))
	sendKey(&m, press('l'))
	sendKey(&m, press('j'))
	if m.cursorCol != 2 || m.cursorRow != 1 {
		t.Fatalf("cursor = (%d,%d), want (1,2)", m.cursorRow, m.cursorCol)
	}
	sendKey(&m, press('h'))
	sendKey(&m, press('k'))
	if m.cursorCol != 1 || m.cursorRow != 0 {
		t.Fatalf("cursor = (%d,%d), want (0,1)", m.cursorRow, m.cursorCol)
	}
}

func TestPaging_IncludingFinalPartialPage(t *testing.T) {
	// height=8 -> headerHeight(1)+footerHeight(1) => visibleRows=6.
	bk := singleBook(100, 2, nil)
	m := NewModel(bk, "sheet1", 40, 8, Options{})
	if vr := m.visibleRows(); vr != 6 {
		t.Fatalf("visibleRows = %d, want 6", vr)
	}

	sendKey(&m, namedKey(tea.KeyPgDown, 0))
	if m.cursorRow != 6 {
		t.Fatalf("after PgDn: cursorRow = %d, want 6", m.cursorRow)
	}

	sendKey(&m, namedKey(tea.KeyPgUp, 0))
	if m.cursorRow != 0 {
		t.Fatalf("after PgUp: cursorRow = %d, want 0", m.cursorRow)
	}
}

func TestPaging_CtrlAliases(t *testing.T) {
	bk := singleBook(100, 2, nil)
	m := NewModel(bk, "sheet1", 40, 8, Options{})

	ctrlF := tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}
	ctrlB := tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
	if got := ctrlF.String(); got != "ctrl+f" {
		t.Fatalf("ctrl+f key String() = %q, want %q", got, "ctrl+f")
	}

	sendKey(&m, ctrlF)
	if m.cursorRow != 6 {
		t.Fatalf("after ctrl+f: cursorRow = %d, want 6", m.cursorRow)
	}
	sendKey(&m, ctrlB)
	if m.cursorRow != 0 {
		t.Fatalf("after ctrl+b: cursorRow = %d, want 0", m.cursorRow)
	}
}

func TestPaging_FinalPartialPageClampsAtLastRow(t *testing.T) {
	bk := singleBook(10, 2, nil) // visibleRows=6 with h=8; two PgDn overshoot 10 rows.
	m := NewModel(bk, "sheet1", 40, 8, Options{})

	ctrlF := tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}
	sendKey(&m, ctrlF)
	sendKey(&m, ctrlF)
	if m.cursorRow != 9 {
		t.Fatalf("cursorRow = %d, want 9 (clamped to nRows-1)", m.cursorRow)
	}
}

func TestFirstLastRowAndColumn(t *testing.T) {
	bk := singleBook(50, 10, nil)
	m := NewModel(bk, "sheet1", 80, 20, Options{})

	sendKey(&m, press('G'))
	if m.cursorRow != 49 {
		t.Fatalf("after G: cursorRow = %d, want 49", m.cursorRow)
	}
	sendKey(&m, press('g'))
	if m.cursorRow != 0 {
		t.Fatalf("after g: cursorRow = %d, want 0", m.cursorRow)
	}

	sendKey(&m, namedKey(tea.KeyEnd, 0))
	if m.cursorCol != 9 {
		t.Fatalf("after End: cursorCol = %d, want 9", m.cursorCol)
	}
	sendKey(&m, namedKey(tea.KeyHome, 0))
	if m.cursorCol != 0 {
		t.Fatalf("after Home: cursorCol = %d, want 0", m.cursorCol)
	}
}

func TestInspectMode_TogglesAndShowsFullValue(t *testing.T) {
	longVal := "this is a very long cell value that will not fit in a narrow column at all"
	bk := singleBook(3, 2, map[int]map[int]string{0: {0: longVal}})
	m := NewModel(bk, "sheet1", 30, 10, Options{MaxColWidth: 8})

	if m.inspect {
		t.Fatalf("inspect should start off")
	}
	sendKey(&m, namedKey(tea.KeyEnter, 0))
	if !m.inspect {
		t.Fatalf("Enter should toggle inspect on")
	}

	// nil exercises footerLine's fallback fetch path (View's normal call
	// passes the row it already fetched; here there is none).
	footer := m.footerLine(nil)
	if !strings.Contains(footer, longVal) {
		t.Fatalf("inspect footer = %q, want it to contain full value %q", footer, longVal)
	}

	// The rendered data cell, by contrast, must be truncated (narrower than
	// the full value).
	dataCell := m.dataLine(0, m.tbl.Row(0))
	if strings.Contains(dataCell, longVal) {
		t.Fatalf("data cell should be truncated, but contains the full value: %q", dataCell)
	}

	sendKey(&m, namedKey(tea.KeyEnter, 0))
	if m.inspect {
		t.Fatalf("second Enter should toggle inspect back off")
	}
}

func TestQuit(t *testing.T) {
	bk := singleBook(3, 2, nil)
	m := NewModel(bk, "sheet1", 40, 10, Options{})

	cmd := sendKey(&m, press('q'))
	if cmd == nil {
		t.Fatalf("q should return a quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("q command produced %T, want tea.QuitMsg", msg)
	}
}

func TestDegenerate_ZeroRows(t *testing.T) {
	bk := singleBook(0, 3, nil)
	m := NewModel(bk, "sheet1", 40, 10, Options{})

	for _, k := range []tea.KeyPressMsg{
		press('j'), press('k'), press('g'), press('G'),
		namedKey(tea.KeyPgDown, 0), namedKey(tea.KeyPgUp, 0),
	} {
		sendKey(&m, k)
	}
	if m.cursorRow != 0 {
		t.Fatalf("cursorRow = %d, want 0 for a zero-row table", m.cursorRow)
	}
	_ = m.View() // must not panic
}

func TestDegenerate_ZeroColumns(t *testing.T) {
	bk := singleBook(3, 0, nil)
	m := NewModel(bk, "sheet1", 40, 10, Options{})

	for _, k := range []tea.KeyPressMsg{
		press('l'), press('h'), namedKey(tea.KeyHome, 0), namedKey(tea.KeyEnd, 0),
	} {
		sendKey(&m, k)
	}
	if m.cursorCol != 0 {
		t.Fatalf("cursorCol = %d, want 0 for a zero-column table", m.cursorCol)
	}
	_ = m.View()
}

func TestDegenerate_OneColumnTerminal(t *testing.T) {
	bk := singleBook(20, 5, nil)
	m := NewModel(bk, "sheet1", 1, 10, Options{})

	for i := 0; i < 10; i++ {
		sendKey(&m, press('l'))
		_ = m.View()
	}
}

func TestDegenerate_TwoRowTerminal(t *testing.T) {
	bk := singleBook(20, 5, nil)
	m := NewModel(bk, "sheet1", 40, 2, Options{})

	for i := 0; i < 10; i++ {
		sendKey(&m, press('j'))
		_ = m.View()
	}
}

func TestDegenerate_ZeroSizeAtStartup(t *testing.T) {
	bk := singleBook(20, 5, nil)
	m := NewModel(bk, "sheet1", 0, 0, Options{})
	_ = m.View()
	sendKey(&m, press('j'))
	_ = m.View()
}

func TestWindowSizeMsg_Grow(t *testing.T) {
	bk := singleBook(100, 3, nil)
	m := NewModel(bk, "sheet1", 40, 8, Options{}) // visibleRows=6

	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	if m.width != 80 || m.height != 30 {
		t.Fatalf("size = (%d,%d), want (80,30)", m.width, m.height)
	}
	if vr := m.visibleRows(); vr != 28 {
		t.Fatalf("visibleRows after grow = %d, want 28", vr)
	}
}

func TestWindowSizeMsg_ShrinkStrandsCursor_PulledBackIntoView(t *testing.T) {
	bk := singleBook(100, 3, nil)
	m := NewModel(bk, "sheet1", 40, 20, Options{}) // visibleRows=18

	m.setCursorRow(40) // scrolls rowOff to 40-18+1=23

	_, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 5}) // visibleRows=3
	if m.cursorRow != 40 {
		t.Fatalf("cursorRow changed on resize: got %d, want 40", m.cursorRow)
	}
	vr := m.visibleRows()
	if m.rowOff > m.cursorRow || m.cursorRow > m.rowOff+vr-1 {
		t.Fatalf("cursor (row %d) stranded outside viewport [%d,%d] after shrink", m.cursorRow, m.rowOff, m.rowOff+vr-1)
	}
}

func TestWindowSizeMsg_ShrinkStrandsCursorColumn(t *testing.T) {
	bk := singleBook(5, 30, nil)
	m := NewModel(bk, "sheet1", 200, 20, Options{})

	m.setCursorCol(25)
	beforeEnd := m.visibleColEnd(m.colOff)
	if 25 >= beforeEnd {
		t.Fatalf("setup invalid: cursorCol 25 not visible before shrink (colOff=%d end=%d)", m.colOff, beforeEnd)
	}

	_, _ = m.Update(tea.WindowSizeMsg{Width: 20, Height: 20})
	if m.cursorCol != 25 {
		t.Fatalf("cursorCol changed on resize: got %d, want 25", m.cursorCol)
	}
	end := m.visibleColEnd(m.colOff)
	if m.cursorCol < m.colOff || m.cursorCol >= end {
		t.Fatalf("cursor column %d stranded outside viewport [%d,%d) after shrink", m.cursorCol, m.colOff, end)
	}
}

func TestHeaderRemainsPresentAtEveryScrollOffset(t *testing.T) {
	bk := singleBook(200, 3, nil)
	m := NewModel(bk, "sheet1", 40, 8, Options{})

	header0 := m.headerLine()
	if header0 == "" {
		t.Fatalf("header line empty at rowOff=0")
	}

	sendKey(&m, press('G')) // jump to the bottom, scrolling rowOff far down.
	if m.rowOff == 0 {
		t.Fatalf("setup invalid: rowOff did not scroll after G")
	}
	headerBottom := m.headerLine()
	if headerBottom != header0 {
		t.Fatalf("header line changed after scrolling: got %q, want %q", headerBottom, header0)
	}

	view := m.View()
	if !strings.Contains(view.Content, "col0") {
		t.Fatalf("rendered view missing header text at scroll offset: %q", view.Content)
	}
}
