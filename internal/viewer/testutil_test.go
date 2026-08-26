package viewer

import (
	"fmt"

	"vanlabeke.dev/exshell/internal/table"
)

// fakeBook is a table.Book test double that supports multiple sheets and,
// critically, sheets that fail to load — table.SingleSheetBook can't express
// that, and the viewer must surface such a failure in the footer rather than
// crash.
type fakeBook struct {
	sheets  []table.SheetInfo
	tables  map[string]table.Table
	failing map[string]bool
}

func (b *fakeBook) Sheets() []table.SheetInfo { return b.sheets }

func (b *fakeBook) Table(name string) (table.Table, error) {
	if b.failing[name] {
		return nil, fmt.Errorf("boom: %s", name)
	}
	t, ok := b.tables[name]
	if !ok {
		return nil, fmt.Errorf("no such sheet %q", name)
	}
	return t, nil
}

// gridTable returns a table.Table with nr rows and nc cols, each cell
// "r<row>c<col>" unless overridden by patch (row -> col -> value).
func gridTable(name string, nr, nc int, patch map[int]map[int]string) table.Table {
	cols := make([]string, nc)
	for c := 0; c < nc; c++ {
		cols[c] = fmt.Sprintf("col%d", c)
	}
	rows := make([][]string, nr)
	for r := 0; r < nr; r++ {
		row := make([]string, nc)
		for c := 0; c < nc; c++ {
			row[c] = fmt.Sprintf("r%dc%d", r, c)
		}
		if p, ok := patch[r]; ok {
			for c, v := range p {
				row[c] = v
			}
		}
		rows[r] = row
	}
	return table.New(name, cols, rows)
}

// singleBook wraps one grid table as a fakeBook with one sheet named
// "sheet1", so single-sheet tests don't need a tab bar.
func singleBook(nr, nc int, patch map[int]map[int]string) *fakeBook {
	t := gridTable("sheet1", nr, nc, patch)
	return &fakeBook{
		sheets: []table.SheetInfo{{Name: "sheet1", Visible: true}},
		tables: map[string]table.Table{"sheet1": t},
	}
}
