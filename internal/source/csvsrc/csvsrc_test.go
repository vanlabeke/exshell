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

// TestLoad_UTF16LargeExport is a realistic-shape regression check: an
// Excel-style "Unicode Text" export (UTF-16LE with a BOM) large enough to
// cross the 64 KB sniff-sample cut, decoded and sniffed end to end.
func TestLoad_UTF16LargeExport(t *testing.T) {
	var b strings.Builder
	b.WriteString("a;b;c\n")
	const wantRows = 6000 // 6000 * 6 UTF-16 code units * 2 bytes > 64 KB sample size
	for i := 0; i < wantRows; i++ {
		b.WriteString("1;2;3\n")
	}
	enc := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM)
	raw, err := enc.NewEncoder().Bytes([]byte(b.String()))
	if err != nil {
		t.Fatalf("encode fixture to utf-16le: %v", err)
	}
	if len(raw) <= sampleSize {
		t.Fatalf("fixture is only %d bytes, want more than sampleSize (%d) to exercise truncation", len(raw), sampleSize)
	}

	tbl, err := Load(bytes.NewReader(raw), "big16.csv", Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := tbl.Cols(); len(got) != 3 || got[0] != "a" {
		t.Fatalf("Cols() = %v, want [a b c] (UTF-16 delimiter sniffing over a truncated sample failed)", got)
	}
	if tbl.NRows() != wantRows {
		t.Fatalf("NRows() = %d, want %d (streaming after the sniff sample lost data)", tbl.NRows(), wantRows)
	}
	last := tbl.Row(wantRows - 1)
	if last[0] != "1" || last[2] != "3" {
		t.Errorf("last row = %v, want [1 2 3]", last)
	}
}

// TestLoad_UTF16TruncatedSampleMidCodeUnit is engineered, byte for byte, to
// fail if the dangling-tail trim ever runs on raw (undecoded) bytes again.
//
// Rows are a fixed 20 raw UTF-16LE bytes each ("1,000;2;3\n", no BOM, no
// header): 3276 of them exactly fill the first 65520 bytes. The next 16
// bytes — still inside the 64 KB sample — are the start of one further
// ("tail") row: "1,000" then U+300A ("《"), whose little-endian encoding is
// the byte pair [0x0A, 0x30]. That 0x0A is, deliberately, both later in the
// sample than every real newline's own 0x0A and NOT a newline at all.
//
// A raw byte-level search for the last 0x0A therefore lands on U+300A's low
// byte instead of on the true final newline. Trimming there (old,
// buggy order: trim-then-decode) keeps "1,000" from the dangling tail row
// as a spurious extra sniff record. Every real row splits on ',' into
// exactly 2 fields ("1,000;2;3" -> "1"/"000;2;3"), which is that
// candidate's own modal count, so the extra "1,000" fragment (-> "1"/"000")
// matches it too and pushes comma's score one above semicolon's — even
// though semicolon has the higher modal field count (3 vs 2), the scoring
// rule compares score first, so comma wins outright. Decoding the full
// (uncorrupted) stream with the wrong delimiter then yields 2 columns, not
// 3.
//
// Decoding before trimming (the fix) reads U+300A corrects, so the trim
// finds the true last decoded '\n' and drops the entire dangling tail row
// — comma and semicolon both score exactly 3276, and semicolon wins the
// count on the modal tie-break, as it must.
func TestLoad_UTF16TruncatedSampleMidCodeUnit(t *testing.T) {
	const goodRow = "1,000;2;3\n"
	const numGoodRows = 3276 // 3276 * 20 bytes = 65520 bytes: just under the 64 KB sample.

	var b strings.Builder
	for i := 0; i < numGoodRows; i++ {
		b.WriteString(goodRow)
	}
	// The dangling tail row. Its first 16 bytes ("1,000" + U+300A + ";2")
	// exactly fill the sample out to the 64 KB mark; the rest of this row,
	// and the fact that there is no BOM, keep the byte-offset arithmetic in
	// the comment above exact.
	b.WriteString("1,000《;2;3\n")

	enc := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	raw, err := enc.NewEncoder().Bytes([]byte(b.String()))
	if err != nil {
		t.Fatalf("encode fixture to utf-16le: %v", err)
	}
	if len(raw) <= sampleSize+1 {
		t.Fatalf("fixture is only %d bytes, want more than sampleSize+1 (%d) to exercise truncation", len(raw), sampleSize+1)
	}
	if raw[65530] != 0x0A || raw[65531] != 0x30 {
		t.Fatalf("fixture byte offsets shifted: raw[65530:65532] = % x, want 0a 30 (U+300A's low/high bytes)", raw[65530:65532])
	}

	tbl, err := Load(bytes.NewReader(raw), "tail16.csv", Options{Encoding: "utf16"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cols := tbl.Cols()
	if len(cols) != 3 {
		t.Fatalf("Cols() = %v, want 3 columns (semicolon); got a comma-shaped result, which means the sniff sample was corrupted by a raw-byte trim before decoding", cols)
	}
	if cols[0] != "1,000" || cols[1] != "2" || cols[2] != "3" {
		t.Errorf("Cols() = %v, want [1,000 2 3]", cols)
	}
	if tbl.NRows() != numGoodRows {
		t.Errorf("NRows() = %d, want %d", tbl.NRows(), numGoodRows)
	}
	last := tbl.Row(numGoodRows - 1)
	if last[0] != "1,000《" || last[1] != "2" || last[2] != "3" {
		t.Errorf("last row = %v, want [1,000《 2 3]", last)
	}
}

// TestLoad_QuotedFieldContainingDelimiter_Comma mirrors
// TestLoad_QuotedFieldContainingDelimiter in the other direction: a
// comma-delimited file with a "Smith, John"-style quoted cell. The sniffing
// mechanism is delimiter-symmetric, but only the semicolon direction was
// covered before this test.
func TestLoad_QuotedFieldContainingDelimiter_Comma(t *testing.T) {
	tbl := loadFixture(t, "quoted_delim_comma.csv", Options{})
	cols := tbl.Cols()
	if len(cols) != 2 {
		t.Fatalf("Cols() = %v, want 2 columns (delimiter sniffing failed)", cols)
	}
	row := tbl.Row(0)
	if row[0] != "Smith, John" {
		t.Errorf("Row(0)[0] = %q, want %q", row[0], "Smith, John")
	}
}

// TestLoad_EncodingUTF8IsAuthoritative asserts that an explicit
// Encoding: "utf8" is never redirected to a different encoding by a foreign
// BOM, while a genuine leading UTF-8 BOM is still stripped.
func TestLoad_EncodingUTF8IsAuthoritative(t *testing.T) {
	t.Run("UTF-16 BOM does not redirect a forced utf8 read", func(t *testing.T) {
		enc := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM)
		raw, err := enc.NewEncoder().Bytes([]byte("name,age\nAlice,30\n"))
		if err != nil {
			t.Fatalf("encode fixture to utf-16le: %v", err)
		}
		// Decoded as UTF-8, this UTF-16LE byte stream is not valid CSV text
		// (it's full of null bytes interleaved with the ASCII content) and
		// must not accidentally look like a clean 2-column table.
		tbl, err := Load(bytes.NewReader(raw), "utf16-mislabeled.csv", Options{Encoding: "utf8"})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := tbl.Cols(); len(got) == 2 && got[0] == "name" && got[1] == "age" {
			t.Fatalf("Cols() = %v; a UTF-16 BOM redirected a forced utf8 read to UTF-16", got)
		}
	})

	t.Run("leading UTF-8 BOM is still stripped when utf8 is explicit", func(t *testing.T) {
		bom := []byte{0xEF, 0xBB, 0xBF}
		content := append(bom, []byte("name,age\nAlice,30\n")...)
		tbl, err := Load(bytes.NewReader(content), "bom.csv", Options{Encoding: "utf8"})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		cols := tbl.Cols()
		if len(cols) != 2 || cols[0] != "name" {
			t.Fatalf("Cols() = %v, want [name age] with BOM stripped", cols)
		}
	})
}

// TestLoad_EncodingUTF16IsAuthoritative asserts that an explicit
// Encoding: "utf16" is never redirected to a different encoding by a
// foreign (UTF-8) BOM — the mirror image of
// TestLoad_EncodingUTF8IsAuthoritative.
func TestLoad_EncodingUTF16IsAuthoritative(t *testing.T) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	content := append(bom, []byte("name,age\nAlice,30\n")...)
	// Interpreted as UTF-16LE, this content (mostly single-byte ASCII plus
	// a 3-byte BOM) will not decode into a clean 2-column "name","age"
	// table, which is the point: a stray UTF-8 BOM must not flip a forced
	// UTF-16 read over to UTF-8.
	tbl, err := Load(bytes.NewReader(content), "utf8-mislabeled.csv", Options{Encoding: "utf16"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := tbl.Cols(); len(got) == 2 && got[0] == "name" && got[1] == "age" {
		t.Fatalf("Cols() = %v; a UTF-8 BOM redirected a forced utf16 read to UTF-8", got)
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
