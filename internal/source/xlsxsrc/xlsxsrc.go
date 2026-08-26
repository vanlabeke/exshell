// Package xlsxsrc implements table.Book on top of an XLSX workbook, using
// excelize to translate raw cell storage into the values a user would see in
// Excel.
package xlsxsrc

import (
	"bytes"
	"fmt"
	"os"

	"github.com/xuri/excelize/v2"

	"vanlabeke.dev/exshell/internal/table"
)

// Workbook wraps an open *excelize.File, building table.Table values from
// its sheets on demand.
type Workbook struct {
	f *excelize.File
}

var _ table.Book = (*Workbook)(nil)

// oleIdentifier is the magic header of the OLE2 Compound File Binary
// container format. It is NOT specific to encryption: it is equally the
// header of a password-encrypted xlsx, a legacy pre-2007 .xls/.doc/.ppt
// file, or any other OLE2 container someone renamed to .xlsx. excelize
// itself checks for this same header before attempting decryption (see
// openReaderAt in its excelize.go); we check it independently so we can
// report a clear, actionable error even in the case excelize does not:
// opening an encrypted file with no password at all fails deep inside its
// decryption path with the generic "unsupported workbook file format",
// which never mentions encryption. But because the header is ambiguous, our
// own message must not claim encryption as fact either — it must cover both
// real possibilities.
var oleIdentifier = []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}

// Open opens the xlsx workbook at path. If the file turns out to be stored
// in the legacy OLE2 binary container (shared by encrypted xlsx and
// pre-2007 .xls/.doc/.ppt files), the returned error describes that
// ambiguity rather than asserting a single cause — see isOLE2Container.
func Open(path string) (*Workbook, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		if isOLE2Container(path) {
			// The underlying err is deliberately dropped here (unlike the
			// generic branch below): it is excelize's low-level zip-parsing
			// failure (e.g. "zip: not a valid zip file"), and appending
			// that after a careful explanation of exactly why this isn't a
			// zip archive at all undermines the explanation — it reads as
			// contradicting itself in its own last clause.
			return nil, fmt.Errorf("%s is stored in the legacy OLE2 binary container format, not a modern xlsx zip archive — this is either a password-protected xlsx (retry with the correct password) or a legacy .xls/.doc/.ppt file (re-save it as .xlsx)", path)
		}
		return nil, fmt.Errorf("could not open %s: %w", path, err)
	}
	return &Workbook{f: f}, nil
}

// isOLE2Container reports whether the file at path starts with the OLE2
// Compound File Binary magic header. This is a container-format signature,
// not proof of encryption: it also matches legacy pre-2007 .xls/.doc/.ppt
// files. Failures reading the file are treated as "not an OLE2 container"
// here; the caller already has a real error to report in that case.
func isOLE2Container(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	header := make([]byte, len(oleIdentifier))
	n, _ := file.Read(header)
	if n < len(oleIdentifier) {
		return false
	}
	return bytes.Equal(header, oleIdentifier)
}

// Sheets returns every sheet in the workbook, in workbook order. Hidden
// sheets are included; callers decide whether to skip them.
func (w *Workbook) Sheets() []table.SheetInfo {
	names := w.f.GetSheetList()
	out := make([]table.SheetInfo, 0, len(names))
	for _, name := range names {
		// GetSheetVisible only errors for a sheet name that doesn't exist,
		// which can't happen here since name came from GetSheetList.
		visible, _ := w.f.GetSheetVisible(name)
		out = append(out, table.SheetInfo{Name: name, Visible: visible})
	}
	return out
}

// Table builds the table.Table for the named sheet on demand. Row 1 is
// treated as the header. Trailing all-empty rows and columns (routine in
// real xlsx files, which often claim a used range far larger than the real
// data) are trimmed; interior empty rows/columns are preserved as-is.
//
// Merged cells are an accepted limitation: excelize stores the value only in
// the merge's top-left cell and reports every other cell in the merge as
// empty, and that is the value exshell shows.
func (w *Workbook) Table(name string) (table.Table, error) {
	if !w.hasSheet(name) {
		return nil, fmt.Errorf("no sheet named %q", name)
	}

	rows, err := w.f.Rows(name)
	if err != nil {
		return nil, fmt.Errorf("reading sheet %q: %w", name, err)
	}
	defer rows.Close()

	var all [][]string
	for rows.Next() {
		cols, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("reading sheet %q: %w", name, err)
		}
		// Columns() returns a copy of the row's cells; nothing else here
		// retains rows' internal state, so it's fine to keep this slice.
		all = append(all, cols)
	}
	if err := rows.Error(); err != nil {
		return nil, fmt.Errorf("reading sheet %q: %w", name, err)
	}

	all = trimTrailingEmpty(all)
	if len(all) == 0 {
		return table.New(name, nil, nil), nil
	}

	header, data := all[0], all[1:]
	return table.New(name, header, data), nil
}

// hasSheet reports whether name is one of the workbook's sheets.
func (w *Workbook) hasSheet(name string) bool {
	for _, n := range w.f.GetSheetList() {
		if n == name {
			return true
		}
	}
	return false
}

// trimTrailingEmpty drops trailing all-empty rows and trailing all-empty
// columns from rows. Rows may be jagged (excelize's Columns skips nothing
// but also never pads), so the "real" width/height of the sheet is
// determined by the last row/column that holds any non-empty value at all.
// Interior empty rows/columns are left untouched; every surviving row is
// padded/truncated to the trimmed width.
func trimTrailingEmpty(rows [][]string) [][]string {
	lastCol := -1
	for _, row := range rows {
		for c, v := range row {
			if v != "" && c > lastCol {
				lastCol = c
			}
		}
	}
	width := lastCol + 1

	lastRow := -1
	for i, row := range rows {
		for c := 0; c < width && c < len(row); c++ {
			if row[c] != "" {
				lastRow = i
				break
			}
		}
	}

	if width == 0 || lastRow == -1 {
		return nil
	}

	out := make([][]string, lastRow+1)
	for i := 0; i <= lastRow; i++ {
		trimmed := make([]string, width)
		copy(trimmed, rows[i])
		out[i] = trimmed
	}
	return out
}

// Close releases the underlying *excelize.File. Calling Close more than once
// is safe.
func (w *Workbook) Close() error {
	if w.f == nil {
		return nil
	}
	f := w.f
	w.f = nil
	return f.Close()
}
