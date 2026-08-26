package viewer

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"vanlabeke.dev/exshell/internal/table"
)

func multiBook() *fakeBook {
	return &fakeBook{
		sheets: []table.SheetInfo{
			{Name: "a", Visible: true},
			{Name: "b", Visible: true},
			{Name: "c", Visible: true},
		},
		tables: map[string]table.Table{
			"a": gridTable("a", 10, 3, nil),
			"b": gridTable("b", 20, 4, nil),
			"c": gridTable("c", 5, 2, nil),
		},
	}
}

func TestSheetSwitching_TabAndShiftTabWrap(t *testing.T) {
	bk := multiBook()
	m := NewModel(bk, "a", 40, 10, Options{})

	sendKey(&m, namedKey(tea.KeyTab, 0))
	if m.active != "b" {
		t.Fatalf("after Tab: active = %q, want b", m.active)
	}
	sendKey(&m, namedKey(tea.KeyTab, 0))
	if m.active != "c" {
		t.Fatalf("after 2nd Tab: active = %q, want c", m.active)
	}
	sendKey(&m, namedKey(tea.KeyTab, 0)) // wraps
	if m.active != "a" {
		t.Fatalf("Tab should wrap past the last sheet: active = %q, want a", m.active)
	}

	sendKey(&m, namedKey(tea.KeyTab, tea.ModShift)) // shift+tab wraps backward
	if m.active != "c" {
		t.Fatalf("shift+Tab should wrap backward: active = %q, want c", m.active)
	}
}

func TestSheetSwitching_ResetsCursorAndViewport(t *testing.T) {
	bk := multiBook()
	m := NewModel(bk, "a", 40, 10, Options{})
	m.setCursorRow(7)
	m.setCursorCol(2)

	sendKey(&m, namedKey(tea.KeyTab, 0))
	if m.cursorRow != 0 || m.cursorCol != 0 {
		t.Fatalf("switching sheets should reset cursor: got (%d,%d)", m.cursorRow, m.cursorCol)
	}
	if m.rowOff != 0 || m.colOff != 0 {
		t.Fatalf("switching sheets should reset viewport: got (%d,%d)", m.rowOff, m.colOff)
	}
	if m.nRows() != 20 || m.nCols() != 4 {
		t.Fatalf("dims after switch = (%d,%d), want (20,4) for sheet b", m.nRows(), m.nCols())
	}
}

func TestSheetSwitching_FailedLoadSurfacesInFooterNotCrash(t *testing.T) {
	bk := multiBook()
	bk.failing = map[string]bool{"b": true}
	m := NewModel(bk, "a", 40, 10, Options{})

	sendKey(&m, namedKey(tea.KeyTab, 0))

	if m.active != "a" {
		t.Fatalf("a failed load must not change the active sheet: active = %q, want a", m.active)
	}
	if m.status == "" || !strings.Contains(m.status, "b") {
		t.Fatalf("status = %q, want it to mention the failed sheet", m.status)
	}
	_ = m.View() // must not panic
}

func TestSheetSwitching_CachesLoadedTables(t *testing.T) {
	loadCount := 0
	bk := multiBook()
	// Wrap Table to count calls for sheet "b".
	orig := bk.tables["b"]
	countingBook := &countingTableBook{fakeBook: bk, target: "b", tbl: orig, count: &loadCount}

	m := NewModel(countingBook, "a", 40, 10, Options{})
	sendKey(&m, namedKey(tea.KeyTab, 0))            // a -> b (load)
	sendKey(&m, namedKey(tea.KeyTab, 0))            // b -> c
	sendKey(&m, namedKey(tea.KeyTab, tea.ModShift)) // c -> b (should be cached)

	if loadCount != 1 {
		t.Fatalf("Table(\"b\") called %d times, want 1 (cached on second visit)", loadCount)
	}
}

// hiddenSheetBook is multiBook with sheet "b" marked hidden, for testing
// that the tab bar visually distinguishes hidden sheets from visible ones
// (R14: hidden sheets stay reachable via Tab, but must not present as an
// equal peer of the visible ones).
func hiddenSheetBook() *fakeBook {
	bk := multiBook()
	bk.sheets = []table.SheetInfo{
		{Name: "a", Visible: true},
		{Name: "b", Visible: false},
		{Name: "c", Visible: true},
	}
	return bk
}

func TestTabBar_HiddenSheetIsMarked(t *testing.T) {
	bk := hiddenSheetBook()
	m := NewModel(bk, "a", 80, 10, Options{})

	bar := m.tabBarLine()
	if !strings.Contains(bar, "b (hidden)") {
		t.Fatalf("tab bar = %q, want the hidden sheet marked as such", bar)
	}
	if strings.Contains(bar, "a (hidden)") || strings.Contains(bar, "c (hidden)") {
		t.Fatalf("tab bar = %q, visible sheets a/c must not be marked hidden", bar)
	}
}

func TestTabBar_HiddenActiveSheetShowsBothMarkerAndActiveStyling(t *testing.T) {
	bk := hiddenSheetBook()
	m := NewModel(bk, "a", 80, 10, Options{})

	barBeforeActive := m.tabBarLine() // "b" rendered hidden but not active

	sendKey(&m, namedKey(tea.KeyTab, 0)) // a -> b: hidden sheet becomes active
	if m.active != "b" {
		t.Fatalf("active = %q, want b", m.active)
	}

	barActive := m.tabBarLine()
	if !strings.Contains(barActive, "(hidden)") {
		t.Fatalf("active-hidden tab bar = %q, hidden marker must survive becoming active", barActive)
	}
	// The active rendering must actually differ from the inactive one (the
	// active-tab style is applied on top of, not instead of, the marker) —
	// otherwise the marker and the active styling would be indistinguishable
	// from a hidden-but-inactive sheet never being selected at all.
	if barActive == barBeforeActive {
		t.Fatalf("tab bar did not change when the hidden sheet became active: %q", barActive)
	}
}

// countingTableBook wraps a fakeBook to count Table() calls for one sheet
// name, so the cache requirement ("switching back is instant") is verifiable
// rather than assumed.
type countingTableBook struct {
	*fakeBook
	target string
	tbl    table.Table
	count  *int
}

func (b *countingTableBook) Table(name string) (table.Table, error) {
	if name == b.target {
		*b.count++
	}
	return b.fakeBook.Table(name)
}
