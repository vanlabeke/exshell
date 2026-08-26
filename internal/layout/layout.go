// Package layout computes column widths, numeric alignment, and
// width-correct truncation/padding for tabular display. It knows nothing
// about files or terminals: given a table.Table and a width budget, it
// returns display-cell measurements. Both the plain-text printer and the
// interactive TUI call this package so they can never disagree about column
// widths.
package layout

import (
	"math"
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

// Unbounded is a sentinel for Options.MaxColWidth and Options.SampleRows
// meaning "no limit at all": every row is measured (SampleRows) and no
// per-column cap is applied (MaxColWidth). It exists for exactly one
// caller: the print path's unconstrained (piped/redirected) output, where
// R18 requires exshell not to lose bytes — see render.Table. It is
// distinct from the zero value (which means "apply the documented
// default") and is never produced by parsing user input: a user-supplied
// --max-col-width is validated at the CLI edge (rejecting anything < 1)
// before it ever reaches Compute, so this sentinel can never collide with
// that validation.
const Unbounded = math.MaxInt

// Sep is the two-space column separator used by both output paths
// (render and viewer), so alignment math never drifts from what is
// actually printed on screen.
const Sep = "  "

// SepCost is the display-cell cost of the Sep separators between n
// columns: len(Sep)*(n-1) for n >= 1, 0 for n <= 1 (no separator without a
// second column).
func SepCost(n int) int {
	if n <= 1 {
		return 0
	}
	return len(Sep) * (n - 1)
}

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
	// MaxColWidth is the per-column cap: 0 applies the documented default
	// (40); Unbounded disables the cap entirely; any other value N clamps
	// every column to at most N. Compute guarantees Col.Width >= 1
	// regardless of what is passed here, even a value below 1 that should
	// never reach Compute in the first place (callers reject that as a
	// usage error before calling in) — the invariant is Compute's to keep,
	// not its callers'.
	MaxColWidth int
	MinColWidth int // shrink floor, default 6 when zero
	// SampleRows is how many data rows are measured for natural width: 0
	// applies the documented default (1000); Unbounded measures every row.
	SampleRows int
	TotalWidth int // 0 = unconstrained; >0 = cells available for column CONTENT
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
		name = Sanitize(name)
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
			cell := Sanitize(row[i])
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
		// The MaxColWidth ceiling is applied before the floor, not after:
		// Compute owns the invariant that Width is always >= 1, and a
		// caller-supplied MaxColWidth below 1 (which should never reach
		// here — see the Options.MaxColWidth doc — would otherwise win the
		// clamp and drive Width negative, which is exactly what used to
		// send render.Table's strings.Repeat into a panic).
		if w > opts.MaxColWidth {
			w = opts.MaxColWidth
		}
		if w < 1 {
			w = 1
		}
		numeric := nonEmpty[i] > 0 && float64(numericHits[i])/float64(nonEmpty[i]) >= 0.8
		resultCols[i] = Col{Name: Sanitize(name), Width: w, Numeric: numeric}
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

// sanitizePlaceholder replaces a stray C0/C1 control byte with something
// visible and exactly one display cell wide, rather than dropping it (which
// would silently shrink the cell) or passing it through (which is how a
// terminal escape sequence embedded in a cell value would reach the real
// terminal).
const sanitizePlaceholder = '�'

// Sanitize maps a raw cell or header value to one safe to both measure and
// print. exshell's whole purpose is displaying files nobody necessarily
// trusts, so nothing may reach a terminal unfiltered: \n, \r, and \t become
// a single space each — a quoted CSV field is allowed to contain a real
// newline (csvsrc preserves it faithfully in the data model, correctly),
// but rendering it as one requires no consumer knows about it, which is
// exactly the bug this closes: an embedded newline used to split one
// logical row across multiple physical lines, breaking the print grid and,
// in the viewer, desynchronising the row-count arithmetic the whole
// viewport depends on. Every other C0 (0x00-0x1F, 0x7F) or C1 (0x80-0x9F)
// control character — including the ESC that starts a terminal escape
// sequence — is replaced with a single placeholder rune, so no cell value
// can inject escape sequences into the terminal, and width is never
// misjudged for a rune runewidth would otherwise score near zero.
//
// Compute calls Sanitize before measuring, so every Col.Width already
// accounts for the sanitized form. render and viewer call Sanitize again
// immediately before formatting a raw cell value with Truncate/Pad, so
// widths and output always agree — this is the one shared place both
// output paths apply it, per internal/layout's whole reason for existing.
func Sanitize(s string) string {
	hasControl := false
	for _, r := range s {
		if isControl(r) {
			hasControl = true
			break
		}
	}
	if !hasControl {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\n', '\r', '\t':
			b.WriteByte(' ')
		default:
			if isControl(r) {
				b.WriteRune(sanitizePlaceholder)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// isControl reports whether r is a C0 control character (0x00-0x1F), DEL
// (0x7F), or a C1 control character (0x80-0x9F).
func isControl(r rune) bool {
	return (r >= 0x00 && r <= 0x1F) || r == 0x7F || (r >= 0x80 && r <= 0x9F)
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
