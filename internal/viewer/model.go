// Package viewer implements the interactive Bubble Tea TUI for exshell: the
// half of the app that appears when a file is too big to dump into
// scrollback. It knows nothing about how data was loaded (that is
// internal/table's job) or how column widths are chosen (internal/layout);
// it only drives the screen.
package viewer

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"vanlabeke.dev/exshell/internal/layout"
	"vanlabeke.dev/exshell/internal/table"
)

// Options controls the interactive viewer.
type Options struct {
	MaxColWidth int
}

// Model is the Bubble Tea model for the interactive viewer. It is exported,
// along with NewModel, specifically so tests can drive it directly with no
// terminal attached: Update is a pure function of (Model, Msg).
type Model struct {
	bk     table.Book
	sheets []table.SheetInfo
	active string
	tables map[string]table.Table // cache: sheet name -> already-loaded Table

	tbl  table.Table
	lay  layout.Layout
	opts Options

	width, height int

	cursorRow, cursorCol int
	rowOff, colOff       int

	inspect bool

	searchActive bool
	searchInput  string
	lastQuery    string

	help bool

	status string // transient footer message: load errors, search results
}

// NewModel constructs a fully usable Model with no terminal present: active
// is loaded immediately via bk.Table, the layout is computed, and the
// viewport is sized to w x h. w and h may be zero (e.g. before the first
// WindowSizeMsg arrives); every dimension-dependent calculation clamps
// against non-positive sizes rather than panicking.
func NewModel(bk table.Book, active string, w, h int, opts Options) Model {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	m := Model{
		bk:     bk,
		sheets: bk.Sheets(),
		opts:   opts,
		width:  w,
		height: h,
		tables: make(map[string]table.Table),
	}
	m.loadSheet(active)
	return m
}

// loadSheet loads name via m.bk.Table (or the cache, so switching back to an
// already-visited sheet is instant), resets cursor/viewport/inspect state,
// and recomputes the layout. A load failure surfaces in m.status and leaves
// the previously active sheet (if any) untouched rather than crashing the
// viewer.
func (m *Model) loadSheet(name string) {
	t, ok := m.tables[name]
	if !ok {
		loaded, err := m.bk.Table(name)
		if err != nil {
			m.status = fmt.Sprintf("error loading sheet %q: %v", name, err)
			return
		}
		m.tables[name] = loaded
		t = loaded
	}

	m.tbl = t
	m.active = name
	m.status = ""
	m.inspect = false
	m.cursorRow, m.cursorCol = 0, 0
	m.rowOff, m.colOff = 0, 0
	m.recomputeLayout()
	m.adjustViewport()
}

// recomputeLayout derives column widths/alignment for the active table via
// layout.Compute. Only MaxColWidth is passed through; TotalWidth is left
// zero deliberately, because the viewer scrolls through columns at their
// natural width rather than shrinking every column to fit one screen (that
// is what the print path's TotalWidth-driven fit does; see render.go).
func (m *Model) recomputeLayout() {
	if m.tbl == nil {
		m.lay = layout.Layout{}
		return
	}
	m.lay = layout.Compute(m.tbl, layout.Options{MaxColWidth: m.opts.MaxColWidth})
}

// switchSheet moves delta positions through m.sheets (wrapping in both
// directions) and loads the target. A book with fewer than two sheets is a
// no-op, matching "the sheet-tab bar only appears when the book has more
// than one sheet."
func (m *Model) switchSheet(delta int) {
	n := len(m.sheets)
	if n < 2 {
		return
	}
	idx := 0
	for i, s := range m.sheets {
		if s.Name == m.active {
			idx = i
			break
		}
	}
	idx = ((idx+delta)%n + n) % n
	m.loadSheet(m.sheets[idx].Name)
}

// nRows and nCols report the current table's dimensions, defaulting to zero
// for a nil table (no sheet successfully loaded yet) so callers never index
// blindly.
func (m *Model) nRows() int {
	if m.tbl == nil {
		return 0
	}
	return m.tbl.NRows()
}

func (m *Model) nCols() int { return len(m.lay.Cols) }

// Screen geometry: one line each for the frozen header and the footer, plus
// one more when a sheet-tab bar is shown.
func (m *Model) headerHeight() int { return 1 }
func (m *Model) footerHeight() int { return 1 }
func (m *Model) tabBarHeight() int {
	if len(m.sheets) > 1 {
		return 1
	}
	return 0
}

// visibleRows is the number of data rows that fit below the frozen header
// and above the footer (and tab bar, if shown). It never goes negative.
func (m *Model) visibleRows() int {
	h := m.height - m.headerHeight() - m.footerHeight() - m.tabBarHeight()
	if h < 0 {
		return 0
	}
	return h
}

// visibleColEnd returns the exclusive end index of the contiguous run of
// columns, starting at colOff, that fit within m.width. Separators between
// visible columns cost 2 cells each (2*(N-1) for N columns), matching
// render.go's budgeting. At least one column is always included when nCols >
// 0, even if its natural width alone exceeds m.width: a degenerate terminal
// clips or overflows visually, but never panics or shows nothing.
func (m *Model) visibleColEnd(colOff int) int {
	cols := m.lay.Cols
	n := len(cols)
	if n == 0 {
		return 0
	}
	if colOff < 0 {
		colOff = 0
	}
	if colOff >= n {
		colOff = n - 1
	}

	used := cols[colOff].Width
	end := colOff + 1
	for end < n {
		cand := used + 2 + cols[end].Width
		if cand > m.width {
			break
		}
		used = cand
		end++
	}
	return end
}

// adjustViewport pulls rowOff/colOff back so the cursor is always within the
// visible window, and clamps both offsets to valid ranges. It is called
// after every cursor move, sheet load, and WindowSizeMsg, which is what
// keeps a cursor that was visible before a resize from ending up outside the
// viewport after it.
func (m *Model) adjustViewport() {
	vr := m.visibleRows()
	nr := m.nRows()
	switch {
	case vr <= 0 || nr == 0:
		m.rowOff = 0
	default:
		if m.cursorRow < m.rowOff {
			m.rowOff = m.cursorRow
		}
		if m.cursorRow > m.rowOff+vr-1 {
			m.rowOff = m.cursorRow - vr + 1
		}
		maxOff := nr - vr
		if maxOff < 0 {
			maxOff = 0
		}
		if m.rowOff > maxOff {
			m.rowOff = maxOff
		}
		if m.rowOff < 0 {
			m.rowOff = 0
		}
	}

	nc := m.nCols()
	if nc == 0 {
		m.colOff = 0
		return
	}
	if m.colOff > nc-1 {
		m.colOff = nc - 1
	}
	if m.colOff < 0 {
		m.colOff = 0
	}
	if m.cursorCol < m.colOff {
		m.colOff = m.cursorCol
	}
	for m.colOff < nc-1 && m.cursorCol >= m.visibleColEnd(m.colOff) {
		m.colOff++
	}
}

// clampInt clamps v to [lo, hi]. Callers are responsible for ensuring
// hi >= lo (i.e. checking the dimension is non-empty) before calling.
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// moveCursor shifts the cursor by (dRow, dCol), clamping at table edges, and
// re-syncs the viewport. A zero-row or zero-column table leaves the
// corresponding cursor coordinate at 0 untouched.
func (m *Model) moveCursor(dRow, dCol int) {
	if nr := m.nRows(); nr > 0 {
		m.cursorRow = clampInt(m.cursorRow+dRow, 0, nr-1)
	}
	if nc := m.nCols(); nc > 0 {
		m.cursorCol = clampInt(m.cursorCol+dCol, 0, nc-1)
	}
	m.adjustViewport()
}

// setCursorRow / setCursorCol jump the cursor to an absolute row/column
// (used by g/G and Home/End), clamping to the valid range.
func (m *Model) setCursorRow(r int) {
	if nr := m.nRows(); nr > 0 {
		m.cursorRow = clampInt(r, 0, nr-1)
	} else {
		m.cursorRow = 0
	}
	m.adjustViewport()
}

func (m *Model) setCursorCol(c int) {
	if nc := m.nCols(); nc > 0 {
		m.cursorCol = clampInt(c, 0, nc-1)
	} else {
		m.cursorCol = 0
	}
	m.adjustViewport()
}

// pageUp / pageDown move the cursor by one screenful of rows. The step is at
// least 1 so paging still makes progress in a terminal too short to show a
// full page (e.g. a 2-row terminal).
func (m *Model) pageUp() {
	step := m.visibleRows()
	if step < 1 {
		step = 1
	}
	m.moveCursor(-step, 0)
}

func (m *Model) pageDown() {
	step := m.visibleRows()
	if step < 1 {
		step = 1
	}
	m.moveCursor(step, 0)
}

var _ tea.Model = (*Model)(nil)
