package viewer

import (
	"fmt"
	"strings"
)

// search performs a case-insensitive substring scan over all cells of the
// active table, starting immediately after (forward) or before (backward)
// the cursor and wrapping around the whole table exactly once. It never
// builds a match index — a linear scan of 100k rows is a few milliseconds,
// and this stays correct even if a streaming backend replaces the
// in-memory one later.
//
// On a hit, the cursor moves to the match and the viewport scrolls it into
// view. On a miss, the cursor is left alone and m.status reports it. Rows
// are fetched at most once each (table.Table.Row allocates per call).
func (m *Model) search(forward bool) {
	q := strings.ToLower(m.lastQuery)
	if q == "" {
		m.status = "no search query"
		return
	}

	nr, nc := m.nRows(), m.nCols()
	total := nr * nc
	if total == 0 {
		m.status = fmt.Sprintf("no match for %q", m.lastQuery)
		return
	}

	step := 1
	if !forward {
		step = -1
	}
	start := m.cursorRow*nc + m.cursorCol

	cachedRow := -1
	var cells []string
	for i := 1; i <= total; i++ {
		pos := ((start+step*i)%total + total) % total
		r, c := pos/nc, pos%nc
		if r != cachedRow {
			cells = m.tbl.Row(r)
			cachedRow = r
		}
		if strings.Contains(strings.ToLower(cells[c]), q) {
			m.cursorRow, m.cursorCol = r, c
			m.adjustViewport()
			m.status = ""
			return
		}
	}

	m.status = fmt.Sprintf("no match for %q", m.lastQuery)
}
