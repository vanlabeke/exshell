package csvsrc

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"

	"vanlabeke.dev/exshell/internal/table"
)

// readFixture returns the raw bytes of a file under testdata/.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// loadFixture loads a testdata file through Load, failing the test on error.
func loadFixture(t *testing.T, name string, opts Options) table.Table {
	t.Helper()
	data := readFixture(t, name)
	tbl, err := Load(bytes.NewReader(data), name, opts)
	if err != nil {
		t.Fatalf("Load(%s): %v", name, err)
	}
	return tbl
}

func TestSniffDelim(t *testing.T) {
	tests := []struct {
		name string
		file string
		want rune
	}{
		{"comma", "comma.csv", ','},
		{"semicolon", "semicolon.csv", ';'},
		{"tab", "tab.csv", '\t'},
		{"pipe", "pipe.csv", '|'},
		{"single column falls back to comma", "single_column.csv", ','},
		{"quoted delimiter", "quoted_delim.csv", ';'},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SniffDelim(readFixture(t, tt.file))
			if got != tt.want {
				t.Errorf("SniffDelim(%s) = %q, want %q", tt.file, got, tt.want)
			}
		})
	}
}

// TestLoad_SemicolonNoFlags is the headline requirement: a semicolon CSV
// exported from a European Excel install must render correctly with no
// flags at all.
func TestLoad_SemicolonNoFlags(t *testing.T) {
	tbl := loadFixture(t, "semicolon.csv", Options{})
	cols := tbl.Cols()
	if len(cols) <= 1 {
		t.Fatalf("expected multiple columns from semicolon file with zero-value Options, got %d: %v", len(cols), cols)
	}
	if len(cols) != 3 {
		t.Errorf("Cols() = %v, want 3 columns", cols)
	}
	if tbl.NRows() != 2 {
		t.Errorf("NRows() = %d, want 2", tbl.NRows())
	}
	if got := tbl.Row(0); got[0] != "Alice" || got[1] != "30" || got[2] != "NYC" {
		t.Errorf("Row(0) = %v, want [Alice 30 NYC]", got)
	}
}

func TestLoad_Comma(t *testing.T) {
	tbl := loadFixture(t, "comma.csv", Options{})
	if got := tbl.Cols(); len(got) != 3 || got[0] != "name" {
		t.Errorf("Cols() = %v", got)
	}
	if tbl.NRows() != 2 {
		t.Errorf("NRows() = %d, want 2", tbl.NRows())
	}
}

func TestLoad_Tab(t *testing.T) {
	tbl := loadFixture(t, "tab.csv", Options{})
	if got := tbl.Cols(); len(got) != 3 {
		t.Errorf("Cols() = %v, want 3 cols", got)
	}
}

func TestLoad_Pipe(t *testing.T) {
	tbl := loadFixture(t, "pipe.csv", Options{})
	if got := tbl.Cols(); len(got) != 3 {
		t.Errorf("Cols() = %v, want 3 cols", got)
	}
}

// TestLoad_QuotedFieldContainingDelimiter asserts that a quoted field
// containing the delimiter stays one field, and that sniffing still picks
// the right delimiter on such a file.
func TestLoad_QuotedFieldContainingDelimiter(t *testing.T) {
	tbl := loadFixture(t, "quoted_delim.csv", Options{})
	cols := tbl.Cols()
	if len(cols) != 3 {
		t.Fatalf("Cols() = %v, want 3 columns (delimiter sniffing failed)", cols)
	}
	row := tbl.Row(0)
	if row[1] != "hello; world" {
		t.Errorf("Row(0)[1] = %q, want %q", row[1], "hello; world")
	}
}

// TestLoad_QuotedFieldContainingNewline asserts that a quoted field
// containing a newline stays one field spanning one record.
func TestLoad_QuotedFieldContainingNewline(t *testing.T) {
	tbl := loadFixture(t, "quoted_newline.csv", Options{})
	if tbl.NRows() != 2 {
		t.Fatalf("NRows() = %d, want 2", tbl.NRows())
	}
	row := tbl.Row(0)
	if row[1] != "line1\nline2" {
		t.Errorf("Row(0)[1] = %q, want %q", row[1], "line1\nline2")
	}
	row1 := tbl.Row(1)
	if row1[0] != "Bob" || row1[1] != "plain" {
		t.Errorf("Row(1) = %v, want [Bob plain]", row1)
	}
}

// TestLoad_ExplicitDelimOverridesSniffing asserts that an explicit Delim
// wins even when sniffing would disagree.
func TestLoad_ExplicitDelimOverridesSniffing(t *testing.T) {
	// semicolon.csv actually uses ';'; sniffing would pick ';'. Force ','
	// instead, which does not appear in the file, so every line should come
	// back as a single field.
	tbl := loadFixture(t, "semicolon.csv", Options{Delim: ','})
	cols := tbl.Cols()
	if len(cols) != 1 {
		t.Fatalf("Cols() = %v, want 1 column (explicit comma delim should not split on ';')", cols)
	}
}

// TestLoad_NoHeader asserts that NoHeader produces A, B, C... labels and
// keeps row 1 as data.
func TestLoad_NoHeader(t *testing.T) {
	tbl := loadFixture(t, "comma.csv", Options{NoHeader: true})
	cols := tbl.Cols()
	want := []string{"A", "B", "C"}
	for i, w := range want {
		if i >= len(cols) || cols[i] != w {
			t.Fatalf("Cols() = %v, want %v", cols, want)
		}
	}
	if tbl.NRows() != 3 {
		t.Fatalf("NRows() = %d, want 3 (original header row is now data)", tbl.NRows())
	}
	if got := tbl.Row(0); got[0] != "name" || got[1] != "age" || got[2] != "city" {
		t.Errorf("Row(0) = %v, want [name age city]", got)
	}
}

// TestLoad_RaggedRows asserts that ragged parser output is handed straight
// to table.New for normalisation, not pre-padded here.
func TestLoad_RaggedRows(t *testing.T) {
	tbl := loadFixture(t, "ragged.csv", Options{})
	cols := tbl.Cols()
	if len(cols) != 4 {
		t.Fatalf("Cols() = %v, want 4 (header extended for the over-long row)", cols)
	}
	if tbl.NRows() != 2 {
		t.Fatalf("NRows() = %d, want 2", tbl.NRows())
	}
	row0 := tbl.Row(0)
	if len(row0) != 4 || row0[2] != "" {
		t.Errorf("Row(0) = %v, want short row padded to width 4", row0)
	}
	row1 := tbl.Row(1)
	if row1[3] != "6" {
		t.Errorf("Row(1) = %v, want last field 6", row1)
	}
}

// TestLoad_HeaderOnly asserts a header-only file is valid: columns present,
// zero data rows.
func TestLoad_HeaderOnly(t *testing.T) {
	tbl := loadFixture(t, "header_only.csv", Options{})
	cols := tbl.Cols()
	if len(cols) != 3 {
		t.Fatalf("Cols() = %v, want 3", cols)
	}
	if tbl.NRows() != 0 {
		t.Errorf("NRows() = %d, want 0", tbl.NRows())
	}
}

// TestLoad_EmptyFileErrors asserts that zero bytes of input is an error
// whose message says the file is empty.
func TestLoad_EmptyFileErrors(t *testing.T) {
	data := readFixture(t, "empty.csv")
	if len(data) != 0 {
		t.Fatalf("fixture empty.csv is not empty: %d bytes", len(data))
	}
	_, err := Load(bytes.NewReader(data), "empty.csv", Options{})
	if err == nil {
		t.Fatal("Load(empty.csv) = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error %q does not mention that the file is empty", err.Error())
	}
}

func TestLoad_SingleColumn(t *testing.T) {
	tbl := loadFixture(t, "single_column.csv", Options{})
	cols := tbl.Cols()
	if len(cols) != 1 || cols[0] != "name" {
		t.Fatalf("Cols() = %v, want [name]", cols)
	}
	if tbl.NRows() != 2 {
		t.Errorf("NRows() = %d, want 2", tbl.NRows())
	}
}

// TestLoad_UTF8BOM asserts a leading UTF-8 BOM is stripped rather than
// polluting the first header cell.
func TestLoad_UTF8BOM(t *testing.T) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	content := append(bom, []byte("name,age\nAlice,30\n")...)
	tbl, err := Load(bytes.NewReader(content), "bom.csv", Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cols := tbl.Cols()
	if len(cols) != 2 || cols[0] != "name" {
		t.Fatalf("Cols() = %v, want [name age] with BOM stripped", cols)
	}
}

// TestLoad_Latin1 asserts latin-1 high bytes round-trip to correct UTF-8.
func TestLoad_Latin1(t *testing.T) {
	utf8Content := "name,city\nRémi,München\n" // Rémi,München
	raw, err := charmap.ISO8859_1.NewEncoder().Bytes([]byte(utf8Content))
	if err != nil {
		t.Fatalf("encode fixture to latin-1: %v", err)
	}

	tbl, err := Load(bytes.NewReader(raw), "latin1.csv", Options{Encoding: "latin1"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	row := tbl.Row(0)
	if row[0] != "Rémi" {
		t.Errorf("Row(0)[0] = %q, want %q", row[0], "Rémi")
	}
	if row[1] != "München" {
		t.Errorf("Row(0)[1] = %q, want %q", row[1], "München")
	}
}

// TestLoad_UTF16 exercises the documented Encoding: "utf16" contract, both
// with a BOM present and forced without one.
func TestLoad_UTF16(t *testing.T) {
	utf8Content := "name,age\nAlice,30\n"

	t.Run("with LE BOM, no explicit encoding", func(t *testing.T) {
		enc := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM)
		raw, err := enc.NewEncoder().Bytes([]byte(utf8Content))
		if err != nil {
			t.Fatalf("encode fixture to utf-16le: %v", err)
		}
		tbl, err := Load(bytes.NewReader(raw), "utf16.csv", Options{})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := tbl.Cols(); len(got) != 2 || got[0] != "name" {
			t.Fatalf("Cols() = %v, want [name age]", got)
		}
	})

	t.Run("no BOM, forced via Encoding option", func(t *testing.T) {
		enc := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
		raw, err := enc.NewEncoder().Bytes([]byte(utf8Content))
		if err != nil {
			t.Fatalf("encode fixture to utf-16le: %v", err)
		}
		tbl, err := Load(bytes.NewReader(raw), "utf16.csv", Options{Encoding: "utf16"})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := tbl.Cols(); len(got) != 2 || got[0] != "name" {
			t.Fatalf("Cols() = %v, want [name age]", got)
		}
	})
}

// TestLoad_LargeFileTruncatedSample exercises the 64 KB sniff-sample cut:
// the sample buffer fills mid-stream, so the dangling final record in the
// sample must be dropped before scoring, and the full stream must still be
// read in its entirety.
func TestLoad_LargeFileTruncatedSample(t *testing.T) {
	var b strings.Builder
	b.WriteString("a;b;c\n")
	const wantRows = 12000 // 12000 * 6 bytes/row > 64KB sample size
	for i := 0; i < wantRows; i++ {
		b.WriteString("1;2;3\n")
	}
	tbl, err := Load(strings.NewReader(b.String()), "big.csv", Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := tbl.Cols(); len(got) != 3 || got[0] != "a" {
		t.Fatalf("Cols() = %v, want [a b c] (delimiter sniffing over a truncated sample failed)", got)
	}
	if tbl.NRows() != wantRows {
		t.Fatalf("NRows() = %d, want %d (streaming after the sniff sample lost data)", tbl.NRows(), wantRows)
	}
	last := tbl.Row(wantRows - 1)
	if last[0] != "1" || last[2] != "3" {
		t.Errorf("last row = %v, want [1 2 3]", last)
	}
}

func TestLoadFile(t *testing.T) {
	tbl, err := LoadFile(filepath.Join("testdata", "comma.csv"), Options{})
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if tbl.Name() != "comma.csv" {
		t.Errorf("Name() = %q, want %q", tbl.Name(), "comma.csv")
	}
}

func TestLoadFile_MissingFile(t *testing.T) {
	_, err := LoadFile(filepath.Join("testdata", "does-not-exist.csv"), Options{})
	if err == nil {
		t.Fatal("LoadFile(missing) = nil error, want an error")
	}
}
