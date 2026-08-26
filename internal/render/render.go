// Package render draws a table.Table as an aligned, pipe-friendly plain-text
// grid: cells joined by two spaces, no box-drawing characters, no outer
// border. It is the print path used both for piped/redirected output and
// for a terminal too small to warrant the interactive viewer.
package render

import (
	"io"
	"strings"

	"vanlabeke.dev/exshell/internal/layout"
	"vanlabeke.dev/exshell/internal/table"
)

// Options controls how Table renders.
//
// MaxColWidth is not part of the brief's terse signature summary for this
// struct, but the brief's prose ("Write MaxColWidth through from the CLI
// flag when set; otherwise leave layout.Options zero values so layout
// applies its own defaults") requires some way for the CLI's
// --max-col-width flag to reach layout.Compute. This field is that path;
// see task-5-report.md for the full note.
type Options struct {
	Width       int  // 0 = unconstrained (piped); >0 = terminal width
	Header      bool // default true; the caller decides whether to set it
	MaxRows     int  // 0 = all
	MaxColWidth int  // 0 = let layout.Options apply its own default (40)
}

// Table writes t to w as an aligned plain-text grid per opts.
//
// Cells are joined by two spaces. When opts.Header is true, the header row
// is followed by a rule line of '-' runs, one per column, matching that
// column's width. Numeric columns are right-aligned; everything else is
// left-aligned, via layout.Pad. Every line ends with a newline, and the
// final column of every line is never right-padded, so lines never carry
// trailing whitespace.
func Table(w io.Writer, t table.Table, opts Options) error {
	cols := t.Cols()
	n := len(cols)

	lopts := layout.Options{MaxColWidth: opts.MaxColWidth}
	if opts.Width > 0 {
		lopts.TotalWidth = opts.Width - sepCost(n)
	}
	lay := layout.Compute(t, lopts)

	if opts.Header {
		if err := writeRow(w, formatRow(cols, lay)); err != nil {
			return err
		}
		if err := writeRow(w, ruleRow(lay)); err != nil {
			return err
		}
	}

	nr := t.NRows()
	if opts.MaxRows > 0 && opts.MaxRows < nr {
		nr = opts.MaxRows
	}
	for r := 0; r < nr; r++ {
		// Fetch the row once; table.Table.Row allocates a fresh copy on
		// every call, and we need each cell exactly once here.
		row := t.Row(r)
		if err := writeRow(w, formatRow(row, lay)); err != nil {
			return err
		}
	}
	return nil
}

// sepCost is the display-cell cost of the two-space separators between N
// columns: 2*(N-1) for N>=1, 0 for N<=1 (no separator without a second
// column).
func sepCost(n int) int {
	if n <= 1 {
		return 0
	}
	return 2 * (n - 1)
}

// formatRow renders one row of raw cell values (a header row or a data row;
// both have len == len(lay.Cols)) into display cells: right-padded numeric
// columns are right-aligned, everything else left-aligned — except the
// final column, which is only ever truncated, never padded, so the line
// never ends in trailing whitespace.
func formatRow(values []string, lay layout.Layout) []string {
	n := len(lay.Cols)
	cells := make([]string, n)
	for i, c := range lay.Cols {
		if i == n-1 && !c.Numeric {
			cells[i] = layout.Truncate(values[i], c.Width)
			continue
		}
		cells[i] = layout.Pad(values[i], c.Width, c.Numeric)
	}
	return cells
}

// ruleRow renders the '-' rule line under the header: one run per column,
// exactly matching that column's width. Dashes are visible content, not
// padding, so the last-column trailing-whitespace exception does not apply
// here.
func ruleRow(lay layout.Layout) []string {
	cells := make([]string, len(lay.Cols))
	for i, c := range lay.Cols {
		cells[i] = strings.Repeat("-", c.Width)
	}
	return cells
}

// writeRow joins cells with two spaces and a trailing newline.
func writeRow(w io.Writer, cells []string) error {
	_, err := io.WriteString(w, strings.Join(cells, "  ")+"\n")
	return err
}
