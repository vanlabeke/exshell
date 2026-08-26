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
// left-aligned, via layout.Pad. Every line ends with a newline, and no line
// ever carries trailing whitespace — writeRow right-trims the fully
// assembled line, which holds regardless of which column's padding would
// otherwise have produced it.
func Table(w io.Writer, t table.Table, opts Options) error {
	cols := t.Cols()
	n := len(cols)

	// noCap marks render.Table's one unconstrained-no-cap case: unbounded
	// output (piped or redirected, opts.Width == 0) with no explicit
	// --max-col-width. It gates two things together: which layout.Options
	// Compute receives below, and which pad primitive formatRow uses for
	// every row (Pad's destructive truncation is wrong here; see
	// PadNoTruncate).
	noCap := opts.Width <= 0 && opts.MaxColWidth == 0

	lopts := layout.Options{MaxColWidth: opts.MaxColWidth}
	switch {
	case opts.Width > 0:
		lopts.TotalWidth = opts.Width - layout.SepCost(n)
	case noCap:
		// Controller ruling R18 — exshell must not lose bytes on the
		// `exshell foo.csv | grep ...` path README.md advertises, so
		// measure every row and drop the default 40-cell cap. An
		// explicitly user-supplied --max-col-width (opts.MaxColWidth > 0,
		// validated >= 1 at the CLI edge) is authoritative and still
		// truncates here exactly as it does everywhere else — it skips
		// this branch entirely.
		//
		// R19 — uncapping Width alone would let one outlier cell inflate
		// every other row's padding to match it (measured: 774x on a real
		// shape). Col.PadWidth stays bounded regardless, and formatRow
		// below uses it (via PadNoTruncate) instead of Width for every
		// value that isn't the rare over-width one.
		lopts.MaxColWidth = layout.Unbounded
		lopts.SampleRows = layout.Unbounded
	}
	lay := layout.Compute(t, lopts)

	if opts.Header {
		if err := writeRow(w, formatRow(cols, lay, noCap)); err != nil {
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
		if err := writeRow(w, formatRow(row, lay, noCap)); err != nil {
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
// column, when it is not numeric, which is only ever truncated, never
// padded, so a left-aligned value never grows trailing spaces of its own. A
// numeric last column still needs padding's right-alignment (padding on
// the left) to look right when stacked against other rows' values of
// different lengths; the case where an empty value turns that padding into
// a cell of nothing but spaces is handled once, robustly, by writeRow's
// final trim — not by refusing to align numeric columns at all.
//
// noCap selects which pad primitive backs that alignment (R19): when true
// (render.Table's unconstrained, no-explicit-cap path), every Pad call
// below becomes a PadNoTruncate call against c.PadWidth instead of
// c.Width, so an ordinary value is padded to the same bounded width it
// would get under the default cap, while a rare over-width value is still
// emitted in full rather than truncated. When false, behavior is unchanged
// from before R19: c.Width and Pad's normal (truncating) behavior.
func formatRow(values []string, lay layout.Layout, noCap bool) []string {
	n := len(lay.Cols)
	cells := make([]string, n)
	for i, c := range lay.Cols {
		v := layout.Sanitize(values[i])
		if i == n-1 && !c.Numeric {
			cells[i] = layout.Truncate(v, c.Width)
			continue
		}
		if noCap {
			cells[i] = layout.PadNoTruncate(v, c.PadWidth, c.Numeric)
			continue
		}
		cells[i] = layout.Pad(v, c.Width, c.Numeric)
	}
	return cells
}

// ruleRow renders the '-' rule line under the header: one run per column,
// matching that column's PadWidth (equal to Width outside R19's
// unconstrained-no-cap path, so this is a no-op change there) rather than
// the possibly-much-larger Width, so the rule line under an outlier column
// stays a sane length instead of stretching to match the one wide row that
// PadWidth deliberately doesn't pad to. Dashes are visible content, not
// padding, so the last-column trailing-whitespace exception does not apply
// here.
func ruleRow(lay layout.Layout) []string {
	cells := make([]string, len(lay.Cols))
	for i, c := range lay.Cols {
		cells[i] = strings.Repeat("-", c.PadWidth)
	}
	return cells
}

// writeRow joins cells with the shared column separator and a trailing
// newline, right-trimming the assembled line before writing. This is the
// single place the "no trailing whitespace" invariant is actually
// enforced, and it holds regardless of *which* cell produced the trailing
// spaces: an empty numeric last cell (all padding, per formatRow), an
// empty non-last cell whose own left-aligned padding becomes trailing once
// everything after it is empty too, an all-empty row, or a value that
// itself ends in a space or a tab (tabs sanitize to a space — see
// layout.Sanitize). Trimming the fully-joined line is a no-op on the rule
// row (which always ends in '-') and on any line that already ends in
// visible content.
func writeRow(w io.Writer, cells []string) error {
	line := strings.TrimRight(strings.Join(cells, layout.Sep), " ")
	_, err := io.WriteString(w, line+"\n")
	return err
}
