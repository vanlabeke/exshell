// Package table defines the core data model shared by every exshell backend
// (CSV, XLSX) and every output path (plain-text printer, TUI).
package table

import "fmt"

// SheetInfo describes one sheet in a Book.
type SheetInfo struct {
	Name    string
	Visible bool
}

// Table is a read-only, offset-indexed view of one 2D dataset: a CSV file or
// a single XLSX sheet. Row is the whole point of this interface: an
// offset-indexed backend for multi-GB files must be able to satisfy it
// without any consumer changing, so implementations must never expose their
// backing slice directly.
type Table interface {
	// Name returns the sheet name, or the base filename for CSV.
	Name() string
	// Cols returns the header labels.
	Cols() []string
	// NRows returns the number of data rows, excluding the header.
	NRows() int
	// Row returns the 0-based row i. len(Row(i)) always equals len(Cols()).
	Row(i int) []string
}

// Book is a collection of named Tables, e.g. the sheets of an XLSX workbook.
type Book interface {
	Sheets() []SheetInfo
	Table(name string) (Table, error)
}

// memTable is an in-memory Table implementation. All data is copied on
// construction and copied again on every read, so callers can never observe
// or corrupt its internal state.
type memTable struct {
	name string
	cols []string
	rows [][]string
}

// New normalises before constructing: width = max(len(cols), longest row).
// cols shorter than width are extended with SyntheticCols names; rows
// shorter than width are padded with "". The returned Table is immutable:
// it copies cols and rows on the way in, and copies rows/cols on the way
// out from Cols/Row.
func New(name string, cols []string, rows [][]string) Table {
	width := len(cols)
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}

	newCols := make([]string, width)
	copy(newCols, cols)
	if width > len(cols) {
		synth := SyntheticCols(width)
		for i := len(cols); i < width; i++ {
			newCols[i] = synth[i]
		}
	}

	newRows := make([][]string, len(rows))
	for i, row := range rows {
		padded := make([]string, width)
		copy(padded, row)
		newRows[i] = padded
	}

	return &memTable{
		name: name,
		cols: newCols,
		rows: newRows,
	}
}

func (t *memTable) Name() string {
	return t.name
}

func (t *memTable) Cols() []string {
	out := make([]string, len(t.cols))
	copy(out, t.cols)
	return out
}

func (t *memTable) NRows() int {
	return len(t.rows)
}

func (t *memTable) Row(i int) []string {
	out := make([]string, len(t.rows[i]))
	copy(out, t.rows[i])
	return out
}

// SyntheticCols returns n Excel-style column labels: A, B, ..., Z, AA, AB,
// ..., AZ, BA, ...
func SyntheticCols(n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = columnLabel(i)
	}
	return out
}

// columnLabel converts a 0-based column index into its Excel-style label.
func columnLabel(i int) string {
	var buf []byte
	for {
		buf = append([]byte{byte('A' + i%26)}, buf...)
		i = i/26 - 1
		if i < 0 {
			break
		}
	}
	return string(buf)
}

// singleSheetBook wraps one Table as a Book with exactly one visible sheet.
type singleSheetBook struct {
	t Table
}

// SingleSheetBook wraps t as a Book with one visible sheet named after
// t.Name().
func SingleSheetBook(t Table) Book {
	return &singleSheetBook{t: t}
}

func (b *singleSheetBook) Sheets() []SheetInfo {
	return []SheetInfo{{Name: b.t.Name(), Visible: true}}
}

func (b *singleSheetBook) Table(name string) (Table, error) {
	if name != b.t.Name() {
		return nil, fmt.Errorf("table: no sheet named %q", name)
	}
	return b.t, nil
}
