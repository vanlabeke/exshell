package table

import (
	"reflect"
	"testing"
)

func TestNewPadsShortRows(t *testing.T) {
	tbl := New("t", []string{"id", "name", "age"}, [][]string{
		{"1", "alice"},
	})
	got := tbl.Row(0)
	want := []string{"1", "alice", ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Row(0) = %#v, want %#v", got, want)
	}
}

func TestNewExtendsHeaderForLongerRow(t *testing.T) {
	tbl := New("t", []string{"id", "name"}, [][]string{
		{"1", "alice", "30", "nyc"},
	})
	wantCols := []string{"id", "name", "C", "D"}
	if got := tbl.Cols(); !reflect.DeepEqual(got, wantCols) {
		t.Fatalf("Cols() = %#v, want %#v", got, wantCols)
	}
	wantRow := []string{"1", "alice", "30", "nyc"}
	if got := tbl.Row(0); !reflect.DeepEqual(got, wantRow) {
		t.Fatalf("Row(0) = %#v, want %#v", got, wantRow)
	}
}

func TestNewDoesNotAliasCallerSlices(t *testing.T) {
	cols := []string{"id", "name"}
	rows := [][]string{
		{"1", "alice"},
	}
	tbl := New("t", cols, rows)

	// Mutate caller's input slices after construction.
	cols[0] = "MUTATED"
	rows[0][0] = "MUTATED"

	if got := tbl.Cols(); got[0] != "id" {
		t.Fatalf("Cols()[0] = %q, want %q (input mutation leaked in)", got[0], "id")
	}
	if got := tbl.Row(0); got[0] != "1" {
		t.Fatalf("Row(0)[0] = %q, want %q (input mutation leaked in)", got[0], "1")
	}

	// Mutate the slice returned by Row/Cols after reading; must not corrupt
	// internal state.
	row := tbl.Row(0)
	row[0] = "MUTATED-OUT"
	if got := tbl.Row(0); got[0] != "1" {
		t.Fatalf("Row(0)[0] after mutating returned slice = %q, want %q", got[0], "1")
	}

	colsOut := tbl.Cols()
	colsOut[0] = "MUTATED-OUT"
	if got := tbl.Cols(); got[0] != "id" {
		t.Fatalf("Cols()[0] after mutating returned slice = %q, want %q", got[0], "id")
	}
}

func TestRowLengthMatchesCols(t *testing.T) {
	tbl := New("t", []string{"a", "b", "c"}, [][]string{
		{"1", "2"},
		{"1", "2", "3", "4"},
		{"1", "2", "3"},
	})
	for i := 0; i < tbl.NRows(); i++ {
		row := tbl.Row(i)
		if len(row) != len(tbl.Cols()) {
			t.Fatalf("Row(%d) len = %d, want %d", i, len(row), len(tbl.Cols()))
		}
	}
}

func TestSyntheticColsBoundaries(t *testing.T) {
	cols := SyntheticCols(53)
	cases := map[int]string{
		0:  "A",
		25: "Z",
		26: "AA",
		51: "AZ",
		52: "BA",
	}
	for idx, want := range cases {
		if got := cols[idx]; got != want {
			t.Fatalf("SyntheticCols(53)[%d] = %q, want %q", idx, got, want)
		}
	}
}

func TestSingleSheetBook(t *testing.T) {
	tbl := New("mytable", []string{"a"}, [][]string{{"1"}})
	book := SingleSheetBook(tbl)

	sheets := book.Sheets()
	if len(sheets) != 1 {
		t.Fatalf("Sheets() len = %d, want 1", len(sheets))
	}
	if sheets[0].Name != "mytable" {
		t.Fatalf("Sheets()[0].Name = %q, want %q", sheets[0].Name, "mytable")
	}
	if !sheets[0].Visible {
		t.Fatalf("Sheets()[0].Visible = false, want true")
	}

	got, err := book.Table("mytable")
	if err != nil {
		t.Fatalf("Table(%q) error = %v, want nil", "mytable", err)
	}
	if got != tbl {
		t.Fatalf("Table(%q) returned a different Table than the wrapped one", "mytable")
	}

	if _, err := book.Table("nope"); err == nil {
		t.Fatalf("Table(%q) error = nil, want an error", "nope")
	}
}

func TestNRowsExcludesHeader(t *testing.T) {
	rows := [][]string{
		{"1"}, {"2"}, {"3"},
	}
	tbl := New("t", []string{"a"}, rows)
	if got := tbl.NRows(); got != len(rows) {
		t.Fatalf("NRows() = %d, want %d", got, len(rows))
	}
}
