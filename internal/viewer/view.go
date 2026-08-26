package viewer

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"vanlabeke.dev/exshell/internal/layout"
)

var (
	headerStyle    = lipgloss.NewStyle().Bold(true).Reverse(true)
	cursorStyle    = lipgloss.NewStyle().Reverse(true)
	activeTabStyle = lipgloss.NewStyle().Bold(true).Reverse(true)
)

// View renders the current model. Screen order, top to bottom: an optional
// sheet-tab bar, the frozen header row, the visible data rows, and a
// footer. The alternate screen is always requested here (never via a
// ProgramOption — v2 moved that switch onto the View itself) and the mouse
// mode is left at its zero value (MouseModeNone), i.e. mouse support off.
func (m *Model) View() tea.View {
	if m.help {
		v := tea.NewView(helpText())
		v.AltScreen = true
		return v
	}

	var b strings.Builder

	if tb := m.tabBarLine(); tb != "" {
		b.WriteString(tb)
		b.WriteByte('\n')
	}

	b.WriteString(m.headerLine())
	b.WriteByte('\n')

	// cursorRowVals captures the cursor row's cells as they're fetched
	// below, so footerLine can reuse them in inspect mode instead of
	// calling table.Row (which allocates) a second time for the same row.
	var cursorRowVals []string

	vr := m.visibleRows()
	nr := m.nRows()
	end := m.rowOff + vr
	if end > nr {
		end = nr
	}
	for r := m.rowOff; r < end; r++ {
		row := m.tbl.Row(r) // fetched once; table.Table.Row allocates per call.
		if r == m.cursorRow {
			cursorRowVals = row
		}
		b.WriteString(m.dataLine(r, row))
		b.WriteByte('\n')
	}

	b.WriteString(m.footerLine(cursorRowVals))

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// tabBarLine renders the sheet-tab bar, or "" when the book has only one
// sheet (in which case View omits the line entirely). A hidden sheet
// (SheetInfo.Visible == false) is still reachable via Tab/shift+Tab — R14:
// exshell is an inspection tool and --sheet already reaches hidden sheets,
// so hiding them from Tab would be inconsistent — but it must not present
// as an equal peer of the visible ones, so its name gets a plain-text
// " (hidden)" suffix. The suffix is baked into the string before
// activeTabStyle is applied, so an active hidden sheet still shows both:
// the style and the marker compose rather than cancel out.
func (m *Model) tabBarLine() string {
	if len(m.sheets) < 2 {
		return ""
	}
	parts := make([]string, len(m.sheets))
	for i, s := range m.sheets {
		name := s.Name
		if !s.Visible {
			name += " (hidden)"
		}
		if s.Name == m.active {
			parts[i] = activeTabStyle.Render(name)
		} else {
			parts[i] = name
		}
	}
	return strings.Join(parts, " | ")
}

// headerLine renders the frozen header for the currently visible column
// range. It never scrolls with rowOff, which is what keeps it "frozen."
func (m *Model) headerLine() string {
	if m.nCols() == 0 {
		return ""
	}
	names := make([]string, m.nCols())
	for i, c := range m.lay.Cols {
		names[i] = c.Name
	}
	return headerStyle.Render(m.formatVisibleRow(names))
}

// dataLine renders one data row from its already-fetched cell values (the
// caller, View, fetches each row exactly once via table.Table.Row, which
// allocates per call), with the cursor cell highlighted when it falls in
// this row.
func (m *Model) dataLine(r int, row []string) string {
	if m.nCols() == 0 {
		return ""
	}

	end := m.visibleColEnd(m.colOff)
	cells := make([]string, 0, end-m.colOff)
	for i := m.colOff; i < end; i++ {
		c := m.lay.Cols[i]
		v := layout.Sanitize(row[i])
		var cell string
		if i == len(m.lay.Cols)-1 {
			cell = layout.Truncate(v, c.Width)
		} else {
			cell = layout.Pad(v, c.Width, c.Numeric)
		}
		if r == m.cursorRow && i == m.cursorCol {
			cell = cursorStyle.Render(cell)
		}
		cells = append(cells, cell)
	}
	return strings.Join(cells, layout.Sep)
}

// formatVisibleRow renders values (one entry per column, header or data)
// across the currently visible column range, following render.go's
// convention: every column is padded to its width and aligned per
// c.Numeric, except when the visible range reaches the table's true last
// column, which is only ever truncated (regardless of c.Numeric) so the
// line never carries trailing whitespace.
func (m *Model) formatVisibleRow(values []string) string {
	end := m.visibleColEnd(m.colOff)
	cells := make([]string, 0, end-m.colOff)
	for i := m.colOff; i < end; i++ {
		c := m.lay.Cols[i]
		v := layout.Sanitize(values[i])
		if i == len(m.lay.Cols)-1 {
			cells = append(cells, layout.Truncate(v, c.Width))
			continue
		}
		cells = append(cells, layout.Pad(v, c.Width, c.Numeric))
	}
	return strings.Join(cells, layout.Sep)
}

// rowRangeText renders the footer's row-position fragment, e.g.
// "rows 41-60/50000", or "rows 0/0" for an empty table.
func (m *Model) rowRangeText() string {
	nr := m.nRows()
	if nr == 0 {
		return "rows 0/0"
	}
	vr := m.visibleRows()
	lo := m.rowOff + 1
	hi := m.rowOff + vr
	if hi > nr {
		hi = nr
	}
	if hi < lo {
		hi = lo
	}
	return fmt.Sprintf("rows %d-%d/%d", lo, hi, nr)
}

// footerLine renders the footer: the search prompt while it is open,
// otherwise the file/sheet name, row position, and current column name (or,
// in inspect mode, the full untruncated value of the current cell instead
// of the column name) plus any transient status message.
//
// cursorRow is the cursor row's cells if View already fetched them this
// pass (it always has, unless the terminal is so short that no data rows
// render at all); reusing it avoids a second table.Row allocation for a
// row View just fetched. When nil, the rare degenerate case is handled by
// fetching it directly.
func (m *Model) footerLine(cursorRow []string) string {
	if m.searchActive {
		return "/" + m.searchInput
	}

	sheet := m.active
	if m.tbl == nil {
		sheet += " (error)"
	}

	if m.inspect {
		val := ""
		if m.nRows() > 0 && m.nCols() > 0 {
			row := cursorRow
			if row == nil {
				row = m.tbl.Row(m.cursorRow)
			}
			val = layout.Sanitize(row[m.cursorCol])
		}
		return fmt.Sprintf("%s | %s | %s", sheet, m.rowRangeText(), val)
	}

	colName := ""
	if m.nCols() > 0 {
		colName = m.lay.Cols[m.cursorCol].Name
	}
	line := fmt.Sprintf("%s | %s | col: %s", sheet, m.rowRangeText(), colName)
	if m.status != "" {
		line += " | " + m.status
	}
	return line
}

// helpText is the full-screen overlay toggled by '?'.
func helpText() string {
	return strings.Join([]string{
		"exshell — keys",
		"",
		"  up/down/left/right, k/j/h/l   move cursor",
		"  PgUp/PgDn, ctrl+b/ctrl+f      page up/down",
		"  g / G                        first / last row",
		"  Home / End                   first / last column",
		"  /                            search (Enter runs, Esc cancels)",
		"  n / N                        next / previous match",
		"  Enter                        toggle inspect mode",
		"  Tab / shift+Tab               next / previous sheet",
		"  ?                            toggle this help",
		"  q, ctrl+c                    quit",
		"",
		"press ? or Esc to close",
	}, "\n")
}
