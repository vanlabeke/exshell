package viewer

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// typeString sends one KeyPressMsg per rune in s, simulating a user typing
// into the search prompt.
func typeString(m *Model, s string) {
	for _, r := range s {
		sendKey(m, press(r))
	}
}

func TestSearch_HitMovesCursorAndScrollsIntoView(t *testing.T) {
	// Small viewport so row 30 is not visible from the top.
	bk := singleBook(100, 4, map[int]map[int]string{30: {2: "the-needle-value"}})
	m := NewModel(bk, "sheet1", 40, 8, Options{}) // visibleRows=6

	sendKey(&m, press('/'))
	if !m.searchActive {
		t.Fatalf("/ should open the search prompt")
	}
	typeString(&m, "needle")
	sendKey(&m, namedKey(tea.KeyEnter, 0))

	if m.searchActive {
		t.Fatalf("Enter should close the search prompt")
	}
	if m.cursorRow != 30 || m.cursorCol != 2 {
		t.Fatalf("cursor = (%d,%d), want (30,2)", m.cursorRow, m.cursorCol)
	}
	vr := m.visibleRows()
	if m.rowOff > m.cursorRow || m.cursorRow > m.rowOff+vr-1 {
		t.Fatalf("match at row %d not scrolled into view [%d,%d]", m.cursorRow, m.rowOff, m.rowOff+vr-1)
	}
}

func TestSearch_MissLeavesCursorAndReports(t *testing.T) {
	bk := singleBook(20, 3, nil)
	m := NewModel(bk, "sheet1", 40, 10, Options{})
	m.setCursorRow(5)

	sendKey(&m, press('/'))
	typeString(&m, "zzz-nonexistent-zzz")
	sendKey(&m, namedKey(tea.KeyEnter, 0))

	if m.cursorRow != 5 {
		t.Fatalf("cursor moved on a miss: cursorRow = %d, want 5", m.cursorRow)
	}
	if m.status == "" {
		t.Fatalf("a miss should report something in the footer status")
	}
}

func TestSearch_NextPreviousTraverseAndWrap(t *testing.T) {
	// Three matches (not two): with only two, forward and backward
	// traversal visit the same alternating sequence and a broken
	// direction (e.g. N silently behaving like n) would still pass. Three
	// matches, starting between them, makes n and N diverge to different
	// rows so the test actually discriminates direction.
	bk := singleBook(20, 2, map[int]map[int]string{
		5:  {0: "needle-a"},
		10: {0: "needle-b"},
		15: {0: "needle-c"},
	})
	m := NewModel(bk, "sheet1", 40, 20, Options{})
	m.cursorRow, m.cursorCol = 8, 1 // between the row-5 and row-10 matches
	m.lastQuery = "needle"

	sendKey(&m, press('n'))
	if m.cursorRow != 10 {
		t.Fatalf("n from row 8: cursorRow = %d, want 10 (next match forward)", m.cursorRow)
	}

	m.cursorRow, m.cursorCol = 8, 1 // reset between the same two matches
	sendKey(&m, press('N'))
	if m.cursorRow != 5 {
		t.Fatalf("N from row 8: cursorRow = %d, want 5 (previous match backward)", m.cursorRow)
	}

	// n wraps to the top at the end.
	m.cursorRow, m.cursorCol = 15, 0
	sendKey(&m, press('n'))
	if m.cursorRow != 5 {
		t.Fatalf("n should wrap to top: cursorRow = %d, want 5", m.cursorRow)
	}

	// N wraps to the bottom at the start.
	m.cursorRow, m.cursorCol = 5, 0
	sendKey(&m, press('N'))
	if m.cursorRow != 15 {
		t.Fatalf("N should wrap to bottom: cursorRow = %d, want 15", m.cursorRow)
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	bk := singleBook(10, 2, map[int]map[int]string{3: {1: "HelloWorld"}})
	m := NewModel(bk, "sheet1", 40, 10, Options{})

	sendKey(&m, press('/'))
	typeString(&m, "helloworld")
	sendKey(&m, namedKey(tea.KeyEnter, 0))

	if m.cursorRow != 3 || m.cursorCol != 1 {
		t.Fatalf("case-insensitive search failed: cursor = (%d,%d), want (3,1)", m.cursorRow, m.cursorCol)
	}
}

func TestSearchPrompt_CharactersAccumulateAndBackspaceDeletes(t *testing.T) {
	bk := singleBook(10, 2, nil)
	m := NewModel(bk, "sheet1", 40, 10, Options{})

	sendKey(&m, press('/'))
	typeString(&m, "abc")
	if m.searchInput != "abc" {
		t.Fatalf("searchInput = %q, want %q", m.searchInput, "abc")
	}
	sendKey(&m, namedKey(tea.KeyBackspace, 0))
	if m.searchInput != "ab" {
		t.Fatalf("searchInput after backspace = %q, want %q", m.searchInput, "ab")
	}
}

func TestSearchPrompt_EscCancelsWithoutMovingCursor(t *testing.T) {
	bk := singleBook(20, 2, map[int]map[int]string{10: {0: "target"}})
	m := NewModel(bk, "sheet1", 40, 10, Options{})
	m.setCursorRow(0)

	sendKey(&m, press('/'))
	typeString(&m, "target")
	sendKey(&m, namedKey(tea.KeyEscape, 0))

	if m.searchActive {
		t.Fatalf("Esc should close the search prompt")
	}
	if m.searchInput != "" {
		t.Fatalf("Esc should clear the pending input, got %q", m.searchInput)
	}
	if m.cursorRow != 0 {
		t.Fatalf("Esc must not move the cursor: cursorRow = %d, want 0", m.cursorRow)
	}
}

func TestSearchPrompt_QAndJAreTextNotCommands(t *testing.T) {
	bk := singleBook(20, 2, nil)
	m := NewModel(bk, "sheet1", 40, 10, Options{})
	m.setCursorRow(3)

	sendKey(&m, press('/'))
	cmdQ := sendKey(&m, press('q'))
	cmdJ := sendKey(&m, press('j'))

	if cmdQ != nil {
		t.Fatalf("q while search prompt is open must not quit")
	}
	if cmdJ != nil {
		t.Fatalf("j while search prompt is open must not produce a command")
	}
	if m.cursorRow != 3 {
		t.Fatalf("j while search prompt is open must not move the cursor: cursorRow = %d, want 3", m.cursorRow)
	}
	if m.searchInput != "qj" {
		t.Fatalf("searchInput = %q, want %q (q and j captured as text)", m.searchInput, "qj")
	}
}

func TestSearch_EmptyQueryDoesNotPanicOrMatch(t *testing.T) {
	bk := singleBook(5, 2, nil)
	m := NewModel(bk, "sheet1", 40, 10, Options{})

	sendKey(&m, press('/'))
	sendKey(&m, namedKey(tea.KeyEnter, 0)) // empty query

	if m.cursorRow != 0 || m.cursorCol != 0 {
		t.Fatalf("empty search should not move the cursor")
	}
}

func TestSearch_ZeroRowsOrColsDoesNotPanic(t *testing.T) {
	bk := singleBook(0, 0, nil)
	m := NewModel(bk, "sheet1", 40, 10, Options{})

	sendKey(&m, press('/'))
	typeString(&m, "anything")
	sendKey(&m, namedKey(tea.KeyEnter, 0))

	if !strings.Contains(m.status, "no match") {
		t.Fatalf("status = %q, want a no-match report", m.status)
	}
}
