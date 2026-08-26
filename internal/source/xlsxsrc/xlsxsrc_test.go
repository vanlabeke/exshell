package xlsxsrc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"vanlabeke.dev/exshell/internal/table"
)

// buildWorkbook creates a new xlsx file in t.TempDir(), lets build populate
// it via excelize's writing API, saves it, and returns the file path.
func buildWorkbook(t *testing.T, build func(f *excelize.File)) string {
	t.Helper()
	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			t.Fatalf("close builder file: %v", err)
		}
	}()

	build(f)

	path := filepath.Join(t.TempDir(), "fixture.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

func TestOpen_SheetsOrderAndVisibility(t *testing.T) {
	path := buildWorkbook(t, func(f *excelize.File) {
		// Sheet1 is created by NewFile.
		if _, err := f.NewSheet("Data"); err != nil {
			t.Fatalf("NewSheet Data: %v", err)
		}
		if _, err := f.NewSheet("Hidden"); err != nil {
			t.Fatalf("NewSheet Hidden: %v", err)
		}
		if err := f.SetSheetVisible("Hidden", false); err != nil {
			t.Fatalf("SetSheetVisible: %v", err)
		}
	})

	wb, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer wb.Close()

	got := wb.Sheets()
	want := []table.SheetInfo{
		{Name: "Sheet1", Visible: true},
		{Name: "Data", Visible: true},
		{Name: "Hidden", Visible: false},
	}
	if len(got) != len(want) {
		t.Fatalf("Sheets() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Sheets()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestTable_DateFormattedCell(t *testing.T) {
	date := time.Date(2026, time.August, 26, 0, 0, 0, 0, time.UTC)

	path := buildWorkbook(t, func(f *excelize.File) {
		must(t, f.SetCellValue("Sheet1", "A1", "ID"))
		must(t, f.SetCellValue("Sheet1", "B1", "Date"))
		must(t, f.SetCellValue("Sheet1", "A2", 1))
		must(t, f.SetCellValue("Sheet1", "B2", date))
	})

	wb, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer wb.Close()

	tbl, err := wb.Table("Sheet1")
	if err != nil {
		t.Fatalf("Table: %v", err)
	}
	if tbl.NRows() != 1 {
		t.Fatalf("NRows() = %d, want 1", tbl.NRows())
	}
	row := tbl.Row(0)
	dateCell := row[1]

	// The headline requirement: this must be a formatted date string, never
	// the raw Excel serial number (46261 for 2026-08-26).
	if dateCell == "46261" {
		t.Fatalf("date cell came back as the raw serial number %q, want a formatted date", dateCell)
	}
	if _, err := strconv.ParseFloat(dateCell, 64); err == nil {
		t.Fatalf("date cell %q parses as a plain number; want a formatted date string", dateCell)
	}
	if !strings.ContainsAny(dateCell, "/-") {
		t.Fatalf("date cell %q does not look like a formatted date (expected a / or - separated date)", dateCell)
	}
	if !strings.Contains(dateCell, "26") {
		t.Fatalf("date cell %q does not appear to contain the expected day/year", dateCell)
	}
}

func TestTable_NumericFormats(t *testing.T) {
	path := buildWorkbook(t, func(f *excelize.File) {
		must(t, f.SetCellValue("Sheet1", "A1", "Price"))
		must(t, f.SetCellValue("Sheet1", "B1", "Share"))

		currencyFmt := `"$"#,##0.00`
		currencyStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: &currencyFmt})
		must(t, err)
		must(t, f.SetCellValue("Sheet1", "A2", 1234.5))
		must(t, f.SetCellStyle("Sheet1", "A2", "A2", currencyStyle))

		pctStyle, err := f.NewStyle(&excelize.Style{NumFmt: 9}) // "0%"
		must(t, err)
		must(t, f.SetCellValue("Sheet1", "B2", 0.5))
		must(t, f.SetCellStyle("Sheet1", "B2", "B2", pctStyle))
	})

	wb, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer wb.Close()

	tbl, err := wb.Table("Sheet1")
	if err != nil {
		t.Fatalf("Table: %v", err)
	}
	row := tbl.Row(0)
	if row[0] != "$1,234.50" {
		t.Errorf("currency cell = %q, want %q", row[0], "$1,234.50")
	}
	if row[1] != "50%" {
		t.Errorf("percentage cell = %q, want %q", row[1], "50%")
	}
}

// TestTable_MergedCells documents the accepted limitation: excelize puts the
// value in the top-left cell of a merge and leaves the rest empty. exshell
// ships that behaviour rather than "fixing" it by broadcasting the value.
func TestTable_MergedCells(t *testing.T) {
	path := buildWorkbook(t, func(f *excelize.File) {
		must(t, f.SetCellValue("Sheet1", "A1", "Header"))
		must(t, f.SetCellValue("Sheet1", "B1", "Other"))
		must(t, f.SetCellValue("Sheet1", "A2", "Merged Value"))
		must(t, f.SetCellValue("Sheet1", "B2", "unrelated"))
		must(t, f.MergeCell("Sheet1", "A2", "B2"))
	})

	wb, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer wb.Close()

	tbl, err := wb.Table("Sheet1")
	if err != nil {
		t.Fatalf("Table: %v", err)
	}
	row := tbl.Row(0)
	if row[0] != "Merged Value" {
		t.Errorf("top-left of merge = %q, want %q", row[0], "Merged Value")
	}
	if row[1] != "" {
		t.Errorf("non-top-left cell of merge = %q, want empty (accepted limitation)", row[1])
	}
}

func TestTable_TrimsTrailingEmptyRange(t *testing.T) {
	path := buildWorkbook(t, func(f *excelize.File) {
		must(t, f.SetCellValue("Sheet1", "A1", "ID"))
		must(t, f.SetCellValue("Sheet1", "B1", "Name"))
		must(t, f.SetCellValue("Sheet1", "A2", "1"))
		must(t, f.SetCellValue("Sheet1", "B2", "Alice"))

		// Simulate the "used range far larger than real data" case: apply a
		// style to a huge trailing range without any values. Excel files
		// routinely carry this from earlier formatting.
		styleID, err := f.NewStyle(&excelize.Style{})
		must(t, err)
		must(t, f.SetCellStyle("Sheet1", "A3", "Z200", styleID))
	})

	wb, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer wb.Close()

	tbl, err := wb.Table("Sheet1")
	if err != nil {
		t.Fatalf("Table: %v", err)
	}
	if got := len(tbl.Cols()); got != 2 {
		t.Errorf("Cols() len = %d, want 2 (trailing empty columns trimmed)", got)
	}
	if tbl.NRows() != 1 {
		t.Errorf("NRows() = %d, want 1 (trailing empty rows trimmed)", tbl.NRows())
	}
}

func TestTable_EmptySheet(t *testing.T) {
	path := buildWorkbook(t, func(f *excelize.File) {
		// Sheet1 is created empty by NewFile; do nothing.
	})

	wb, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer wb.Close()

	tbl, err := wb.Table("Sheet1")
	if err != nil {
		t.Fatalf("Table on empty sheet should not error: %v", err)
	}
	if len(tbl.Cols()) != 0 {
		t.Errorf("Cols() = %v, want empty", tbl.Cols())
	}
	if tbl.NRows() != 0 {
		t.Errorf("NRows() = %d, want 0", tbl.NRows())
	}
}

func TestOpen_EncryptedWorkbook(t *testing.T) {
	f := excelize.NewFile()
	must(t, f.SetCellValue("Sheet1", "A1", "secret"))

	path := filepath.Join(t.TempDir(), "encrypted.xlsx")
	if err := f.SaveAs(path, excelize.Options{Password: "secret"}); err != nil {
		t.Fatalf("SaveAs with password: %v", err)
	}
	must(t, f.Close())

	_, err := Open(path)
	if err == nil {
		t.Fatal("Open on encrypted workbook: want error, got nil")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "encrypt") && !strings.Contains(msg, "password") {
		t.Fatalf("Open error %q does not mention encryption or password", err.Error())
	}
}

// TestOpen_LegacyBinaryContainer covers the case that surprised the first
// implementation of Open: the OLE2 Compound File Binary magic header is a
// *container* signature, not an encryption marker. It is equally the header
// of an unencrypted legacy pre-2007 .xls/.doc/.ppt file. A user who points
// Open at an ordinary .xls (wrong file picked, or a renamed extension) must
// not be told with confidence that the file is password-protected — that
// sends them hunting for a password that doesn't exist. The error must
// describe what was actually detected (an OLE2 container) and cover both
// real possibilities, not assert one as fact.
func TestOpen_LegacyBinaryContainer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.xls")
	// The 8-byte OLE2 magic header followed by filler bytes is enough to
	// trigger excelize's OLE2 detection path without needing a real,
	// well-formed .xls file.
	content := append([]byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}, make([]byte, 64)...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := Open(path)
	if err == nil {
		t.Fatal("Open on legacy OLE2 binary file: want error, got nil")
	}

	msg := strings.ToLower(err.Error())
	// It's fine, even necessary, for the message to mention password/
	// encryption as one possibility for an encrypted-xlsx user's benefit...
	if !strings.Contains(msg, "encrypt") && !strings.Contains(msg, "password") {
		t.Fatalf("Open error %q does not mention encryption or password as a possibility", err.Error())
	}
	// ...but it must not assert that the file *is* password-protected as an
	// established fact, since here it plainly isn't.
	if strings.Contains(msg, "is password-protected") || strings.Contains(msg, "appears to be password-protected") {
		t.Fatalf("Open error %q asserts password-protection as fact for a non-encrypted legacy file", err.Error())
	}
	// It should name the actual thing it detected, and the other real
	// explanation, so the message is actionable either way.
	if !strings.Contains(msg, "ole2") && !strings.Contains(msg, "legacy") {
		t.Fatalf("Open error %q does not describe the OLE2/legacy-container ambiguity", err.Error())
	}
}

func TestTable_UnknownSheetName(t *testing.T) {
	path := buildWorkbook(t, func(f *excelize.File) {})

	wb, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer wb.Close()

	_, err = wb.Table("DoesNotExist")
	if err == nil {
		t.Fatal("Table with unknown sheet name: want error, got nil")
	}
}

func TestClose_Idempotent(t *testing.T) {
	path := buildWorkbook(t, func(f *excelize.File) {})

	wb, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := wb.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := wb.Close(); err != nil {
		t.Fatalf("second Close should not error, got: %v", err)
	}
}

func TestWorkbookSatisfiesTableBook(t *testing.T) {
	var _ table.Book = (*Workbook)(nil)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
