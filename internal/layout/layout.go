// Package layout computes column widths, numeric alignment, and
// width-correct truncation/padding for tabular display. It knows nothing
// about files or terminals: given a table.Table and a width budget, it
// returns display-cell measurements. Both the plain-text printer and the
// interactive TUI call this package so they can never disagree about column
// widths.
package layout

import (
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
	"vanlabeke.dev/exshell/internal/table"
)

// defaults for zero-valued Options fields.
const (
	defaultMaxColWidth = 40
	defaultMinColWidth = 6
	defaultSampleRows  = 1000
)

// Col describes one computed column.
type Col struct {
	Name    string
	Width   int  // display cells
	Numeric bool // right-align
}

// Layout is the computed set of columns for a table.
type Layout struct{ Cols []Col }

// Options controls Compute. Zero values take documented defaults.
type Options struct {
	MaxColWidth int // default 40 when zero
	MinColWidth int // shrink floor, default 6 when zero
	SampleRows  int // default 1000 when zero
	TotalWidth  int // 0 = unconstrained; >0 = cells available for column CONTENT
}

// currencyPrefixes are the leading currency symbols stripped before parsing
// a cell as numeric.
var currencyPrefixes = []string{"$", "€", "£"}

// Compute derives per-column display widths and numeric alignment for t.
//
// Natural width is, per column, the max display width over the header label
// and the first SampleRows data rows, clamped to [1, MaxColWidth]. When
// TotalWidth > 0 and the natural widths sum to more than TotalWidth, columns
// are shrunk deterministically (see fit) down to MinColWidth.
func Compute(t table.Table, opts Options) Layout {
	if opts.MaxColWidth == 0 {
		opts.MaxColWidth = defaultMaxColWidth
	}
	if opts.MinColWidth == 0 {
		opts.MinColWidth = defaultMinColWidth
	}
	if opts.SampleRows == 0 {
		opts.SampleRows = defaultSampleRows
	}

	cols := t.Cols()
	n := len(cols)

	widths := make([]int, n)
	nonEmpty := make([]int, n)
	numericHits := make([]int, n)

	for i, name := range cols {
		if w := runewidth.StringWidth(name); w > widths[i] {
			widths[i] = w
		}
	}

	sampleN := opts.SampleRows
	if nr := t.NRows(); sampleN > nr {
		sampleN = nr
	}

	for r := 0; r < sampleN; r++ {
		row := t.Row(r)
		for i := 0; i < n; i++ {
			cell := row[i]
			if w := runewidth.StringWidth(cell); w > widths[i] {
				widths[i] = w
			}
			if cell == "" {
				continue
			}
			nonEmpty[i]++
			if isNumericCell(cell) {
				numericHits[i]++
			}
		}
	}

	resultCols := make([]Col, n)
	for i, name := range cols {
		w := widths[i]
		if w < 1 {
			w = 1
		}
		if w > opts.MaxColWidth {
			w = opts.MaxColWidth
		}
		numeric := nonEmpty[i] > 0 && float64(numericHits[i])/float64(nonEmpty[i]) >= 0.8
		resultCols[i] = Col{Name: name, Width: w, Numeric: numeric}
	}

	if opts.TotalWidth > 0 {
		fit(resultCols, opts.TotalWidth, opts.MinColWidth)
	}

	return Layout{Cols: resultCols}
}

// fit shrinks resultCols in place, deterministically, until the sum of
// widths fits within totalWidth or every column has been floored at
// minColWidth. On each step the currently-widest column shrinks by 1; ties
// are broken by the lowest index. No map iteration is used, so the result is
// byte-identical for a given input every time.
func fit(cols []Col, totalWidth, minColWidth int) {
	for {
		sum := 0
		for _, c := range cols {
			sum += c.Width
		}
		if sum <= totalWidth {
			return
		}

		maxIdx := -1
		maxWidth := -1
		for i, c := range cols {
			if c.Width <= minColWidth {
				continue
			}
			if c.Width > maxWidth {
				maxWidth = c.Width
				maxIdx = i
			}
		}
		if maxIdx == -1 {
			// Every column is already at or below the floor; cannot shrink
			// further. The overflow is the renderer's problem.
			return
		}
		cols[maxIdx].Width--
	}
}

// isNumericCell reports whether s parses as a number once spaces, commas,
// percent signs, and a leading currency symbol are stripped.
func isNumericCell(s string) bool {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "%", "")
	for _, p := range currencyPrefixes {
		s = strings.TrimPrefix(s, p)
	}
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// ellipsis is the marker Truncate appends when it trims a string. It is a
// single display cell wide.
const ellipsis = "…"

// Truncate returns s fitted into exactly w display cells. If s already fits,
// it is returned unchanged. Otherwise the result is exactly w cells
// including a trailing "…": wide runes are dropped whole, never split, and
// if dropping one leaves a spare cell the result is padded with spaces so it
// is still exactly w cells. Truncate("", w) with w < 1 returns "".
func Truncate(s string, w int) string {
	if w < 1 {
		return ""
	}
	if runewidth.StringWidth(s) <= w {
		return s
	}

	target := w - 1 // cells available for content before the ellipsis
	var b strings.Builder
	width := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if width+rw > target {
			break
		}
		b.WriteRune(r)
		width += rw
	}
	// A dropped wide rune can leave the content short of target by one cell;
	// pad before the ellipsis so it always ends the string.
	for width < target {
		b.WriteByte(' ')
		width++
	}
	b.WriteString(ellipsis)
	return b.String()
}

// Pad returns s fitted into exactly w display cells, aligned right when
// right is true and left otherwise. Input wider than w is truncated first so
// the result never overflows w cells.
func Pad(s string, w int, right bool) string {
	if w < 1 {
		return ""
	}

	sw := runewidth.StringWidth(s)
	if sw > w {
		s = Truncate(s, w)
		sw = runewidth.StringWidth(s)
	}
	if sw >= w {
		return s
	}

	padding := strings.Repeat(" ", w-sw)
	if right {
		return padding + s
	}
	return s + padding
}
