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
	switch {
	case opts.Width > 0:
		lopts.TotalWidth = opts.Width - layout.SepCost(n)
	case opts.MaxColWidth == 0:
		// Unconstrained output (piped or redirected) with no explicit
		// --max-col-width: controller ruling R18 — exshell must not lose
		// bytes on the `exshell foo.csv | grep ...` path README.md
		// advertises, so measure every row and drop the default 40-cell
		// cap. An explicitly user-supplied --max-col-width (opts.MaxColWidth
		// > 0, validated >= 1 at the CLI edge) is authoritative and still
		// truncates here exactly as it does everywhere else.
		lopts.MaxColWidth = layout.Unbounded
		lopts.SampleRows = layout.Unbounded
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

// formatRow renders one row of raw cell values (a header row or a data row;
// both have len == len(lay.Cols)) into display cells: each value is
// sanitized (see layout.Sanitize) before formatting, so what is measured
// and what is emitted always agree. Right-aligned numeric columns are
// padded on the left, everything else on the right — except the final
// column, which is only ever truncated, never padded (regardless of
// Numeric: a numeric column padded on the left produces nothing but
// trailing spaces when its value is empty, which is exactly the invariant
// this exception protects), so the line never ends in trailing whitespace.
func formatRow(values []string, lay layout.Layout) []string {
	n := len(lay.Cols)
	cells := make([]string, n)
	for i, c := range lay.Cols {
		v := layout.Sanitize(values[i])
		if i == n-1 {
			cells[i] = layout.Truncate(v, c.Width)
			continue
		}
		cells[i] = layout.Pad(v, c.Width, c.Numeric)
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

// writeRow joins cells with the shared column separator and a trailing
// newline. When the final cell is empty (formatRow's last-column exception
// produces exactly that for an empty value, never padding), its preceding
// separator is dropped too — strings.Join alone would still place a
// separator right before an empty final element, which is trailing
// whitespace by another name.
func writeRow(w io.Writer, cells []string) error {
	if n := len(cells); n > 0 && cells[n-1] == "" {
		cells = cells[:n-1]
	}
	_, err := io.WriteString(w, strings.Join(cells, layout.Sep)+"\n")
	return err
}
