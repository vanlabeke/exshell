package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"vanlabeke.dev/exshell/internal/table"
)

// --- chooseMode: exhaustive precedence + fit matrix ---------------------

func TestChooseMode(t *testing.T) {
	cases := []struct {
		name                                string
		forcePrint, forceInteractive, isTTY bool
		termW, termH, needW, needH          int
		want                                Mode
	}{
		{"forcePrint wins over everything, non-tty small", true, false, false, 80, 24, 5, 5, ModePrint},
		{"forcePrint wins over forceInteractive both set", true, true, true, 80, 24, 500, 500, ModePrint},
		{"forceInteractive wins over non-tty", false, true, false, 80, 24, 5, 5, ModeViewer},
		{"forceInteractive wins even when it fits", false, true, true, 80, 24, 5, 5, ModeViewer},
		{"no force, not a tty, tiny content", false, false, false, 80, 24, 5, 5, ModePrint},
		{"no force, not a tty, huge content", false, false, false, 80, 24, 5000, 5000, ModePrint},
		{"tty, fits exactly at boundary", false, false, true, 80, 24, 80, 23, ModePrint},
		{"tty, fits with room to spare", false, false, true, 80, 24, 10, 5, ModePrint},
		{"tty, one cell too wide", false, false, true, 80, 24, 81, 23, ModeViewer},
		{"tty, one line too tall", false, false, true, 80, 24, 80, 24, ModeViewer},
		{"tty, too wide and too tall", false, false, true, 80, 24, 200, 200, ModeViewer},
		{"tty, needH exactly termH (needs the -1 margin)", false, false, true, 80, 24, 80, 24, ModeViewer},
		{"tty, needH termH-1 fits", false, false, true, 80, 24, 80, 23, ModePrint},
		// C1: an unmeasurable terminal (GetSize failed, or genuinely
		// reported 0x0 — the `script`/pty-without-winsize repro from the
		// brief) must never fall through to the viewer, even though
		// needW<=0 or needH<=-1 is never true for a real table, which is
		// exactly what let the original bug slip through this matrix
		// undetected: every prior case ran at termW=80, termH=24, so the
		// one boundary that mattered was never exercised.
		{"tty, unmeasurable 0x0, no force", false, false, true, 0, 0, 12, 3, ModePrint},
		{"tty, unmeasurable termW=0 only", false, false, true, 0, 24, 12, 3, ModePrint},
		{"tty, unmeasurable termH=0 only", false, false, true, 80, 0, 12, 3, ModePrint},
		{"tty, unmeasurable negative termH", false, false, true, 80, -1, 12, 3, ModePrint},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := chooseMode(c.forcePrint, c.forceInteractive, c.isTTY, c.termW, c.termH, c.needW, c.needH)
			if got != c.want {
				t.Errorf("chooseMode(%v,%v,%v,%d,%d,%d,%d) = %v, want %v",
					c.forcePrint, c.forceInteractive, c.isTTY, c.termW, c.termH, c.needW, c.needH, got, c.want)
			}
		})
	}
}

// --- termSizeResult: the pure (getSizeErr, w, h) -> (isTTY, w, h) mapping,
// factored out of termInfo specifically so this case is unit-testable
// without a real terminal file descriptor. ---------------------------

func TestTermSizeResult(t *testing.T) {
	boom := fmt.Errorf("boom")
	cases := []struct {
		name         string
		err          error
		w, h         int
		wantIsTTY    bool
		wantW, wantH int
	}{
		{"success reports real dimensions", nil, 80, 24, true, 80, 24},
		{"success reporting a genuine 0x0 window is passed through as-is", nil, 0, 0, true, 0, 0},
		{"GetSize error maps to isTTY true, 0x0", boom, 0, 0, true, 0, 0},
		{"GetSize error discards whatever stale w/h it returned", boom, 999, 999, true, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotTTY, gotW, gotH := termSizeResult(c.err, c.w, c.h)
			if gotTTY != c.wantIsTTY || gotW != c.wantW || gotH != c.wantH {
				t.Errorf("termSizeResult(%v, %d, %d) = (%v,%d,%d), want (%v,%d,%d)",
					c.err, c.w, c.h, gotTTY, gotW, gotH, c.wantIsTTY, c.wantW, c.wantH)
			}
		})
	}
}

// --- isXLSXPath: extension + content sniffing ---------------------------

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestIsXLSXPath(t *testing.T) {
	dir := t.TempDir()

	xlsxExt := writeFile(t, dir, "book.xlsx", []byte("not really a zip but extension says xlsx"))
	plainCSV := writeFile(t, dir, "plain.csv", []byte("a,b,c\n1,2,3\n"))
	misnamedZip := writeFile(t, dir, "misnamed.csv", []byte("PK\x03\x04rest of zip content"))
	unknownExtZip := writeFile(t, dir, "data.dat", []byte("PK\x03\x04rest of zip content"))

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"xlsx extension wins even without zip magic", xlsxExt, true},
		{"plain csv content and extension", plainCSV, false},
		{"zip magic overrides a .csv extension", misnamedZip, true},
		{"zip magic with an unrecognized extension", unknownExtZip, true},
		{"missing file falls back to false", filepath.Join(dir, "missing.csv"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isXLSXPath(c.path); got != c.want {
				t.Errorf("isXLSXPath(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

// --- resolveSheet ---------------------------------------------------------

func TestResolveSheet(t *testing.T) {
	sheets := []table.SheetInfo{
		{Name: "Hidden", Visible: false},
		{Name: "Data", Visible: true},
		{Name: "Extra", Visible: true},
	}

	cases := []struct {
		name    string
		want    string
		outName string
		wantErr bool
	}{
		{"empty selects first visible sheet", "", "Data", false},
		{"1-based index picks that sheet", "1", "Hidden", false},
		{"index out of range errors", "99", "", true},
		{"literal name match", "Extra", "Extra", false},
		{"unknown literal name errors", "NoSuchSheet", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveSheet(sheets, c.want)
			if c.wantErr {
				if err == nil {
					t.Fatalf("resolveSheet(%q) = %q, nil; want error", c.want, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveSheet(%q): unexpected error: %v", c.want, err)
			}
			if got != c.outName {
				t.Errorf("resolveSheet(%q) = %q, want %q", c.want, got, c.outName)
			}
		})
	}
}

// --- end-to-end Run() ------------------------------------------------------

func runCLI(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = Run(args, &out, &errBuf)
	return out.String(), errBuf.String(), code
}

// buildXLSX builds an xlsx workbook and saves it at path. excelize's
// SaveAs refuses non-.xlsx extensions, so when path doesn't end in .xlsx
// (used to test that content sniffing, not the extension, decides the
// reader) it saves to a real .xlsx path first and renames the file into
// place — the bytes on disk are unaffected by the rename.
func buildXLSX(t *testing.T, path string, build func(f *excelize.File)) {
	t.Helper()
	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			t.Fatalf("close builder file: %v", err)
		}
	}()
	build(f)

	savePath := path
	if filepath.Ext(path) != ".xlsx" {
		savePath = path + "-builder.xlsx"
	}
	if err := f.SaveAs(savePath); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	if savePath != path {
		if err := os.Rename(savePath, path); err != nil {
			t.Fatalf("rename %s to %s: %v", savePath, path, err)
		}
	}
}

func TestRun_CSVPrints(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "t.csv", []byte("id,name\n1,Bob\n2,Ann\n"))

	stdout, stderr, code := runCLI(t, path)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	want := "id  name\n--  ----\n 1  Bob\n 2  Ann\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestRun_ListSheets_CSV(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "data.csv", []byte("a,b\n1,2\n"))

	stdout, stderr, code := runCLI(t, "--list-sheets", path)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if stdout != "data.csv\n" {
		t.Errorf("stdout = %q, want %q", stdout, "data.csv\n")
	}
}

func TestRun_ListSheets_XLSX(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.xlsx")
	buildXLSX(t, path, func(f *excelize.File) {
		if _, err := f.NewSheet("Data"); err != nil {
			t.Fatalf("NewSheet: %v", err)
		}
	})

	stdout, stderr, code := runCLI(t, "--list-sheets", path)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if stdout != "Sheet1\nData\n" {
		t.Errorf("stdout = %q, want %q", stdout, "Sheet1\nData\n")
	}
}

func TestRun_MissingFileExitsOne(t *testing.T) {
	stdout, stderr, code := runCLI(t, "/no/such/path/does-not-exist.csv")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stdout=%q stderr=%q)", code, stdout, stderr)
	}
	if !strings.HasPrefix(stderr, "exshell: ") {
		t.Errorf("stderr = %q, want it to start with %q", stderr, "exshell: ")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestRun_UnknownFlagExitsTwo(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "t.csv", []byte("a,b\n1,2\n"))

	_, stderr, code := runCLI(t, "--totally-bogus-flag", path)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (stderr=%q)", code, stderr)
	}
	if !strings.HasPrefix(stderr, "exshell: ") {
		t.Errorf("stderr = %q, want it to start with %q", stderr, "exshell: ")
	}
}

func TestRun_NoFileArgumentExitsTwo(t *testing.T) {
	_, stderr, code := runCLI(t)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (stderr=%q)", code, stderr)
	}
}

func TestRun_TooManyFileArgumentsExitsTwo(t *testing.T) {
	dir := t.TempDir()
	p1 := writeFile(t, dir, "a.csv", []byte("a\n1\n"))
	p2 := writeFile(t, dir, "b.csv", []byte("a\n1\n"))

	_, stderr, code := runCLI(t, p1, p2)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (stderr=%q)", code, stderr)
	}
}

func TestRun_BadSheetNameExitsTwo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.xlsx")
	buildXLSX(t, path, func(f *excelize.File) {})

	_, stderr, code := runCLI(t, "--sheet", "NoSuchSheet", path)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (stderr=%q)", code, stderr)
	}
	if !strings.HasPrefix(stderr, "exshell: ") {
		t.Errorf("stderr = %q, want it to start with %q", stderr, "exshell: ")
	}
}

func TestRun_MisnamedXLSXDetectedByContent(t *testing.T) {
	dir := t.TempDir()
	// Deliberately given a .csv extension: content sniffing must still
	// route this through the xlsx reader.
	path := filepath.Join(dir, "actually-xlsx.csv")
	buildXLSX(t, path, func(f *excelize.File) {
		f.SetCellValue("Sheet1", "A1", "Name")
		f.SetCellValue("Sheet1", "B1", "Score")
		f.SetCellValue("Sheet1", "A2", "Alice")
		f.SetCellValue("Sheet1", "B2", 42)
	})

	stdout, stderr, code := runCLI(t, path)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Name") || !strings.Contains(stdout, "Score") {
		t.Errorf("stdout missing expected xlsx header content: %q", stdout)
	}
	if !strings.Contains(stdout, "Alice") || !strings.Contains(stdout, "42") {
		t.Errorf("stdout missing expected xlsx data content: %q", stdout)
	}
}

func TestRun_Version(t *testing.T) {
	stdout, stderr, code := runCLI(t, "--version")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "exshell") {
		t.Errorf("stdout = %q, want it to mention exshell", stdout)
	}
}

// TestRun_ForceInteractiveWithoutTTYExitsOneWithMessage pins that --interactive
// against a non-TTY destination (the only kind a test harness can drive) exits
// 1 with an "exshell: "-prefixed message: Bubble Tea itself fails fast when it
// can't acquire a real terminal, and the CLI surfaces that failure like any
// other runtime error rather than crashing or hanging.
func TestRun_ForceInteractiveWithoutTTYExitsOneWithMessage(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "t.csv", []byte("a\n1\n"))

	stdout, stderr, code := runCLI(t, "--interactive", path)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stdout=%q stderr=%q)", code, stdout, stderr)
	}
	if !strings.HasPrefix(stderr, "exshell: ") {
		t.Errorf("stderr = %q, want it to start with %q", stderr, "exshell: ")
	}
}

// TestRun_EmptyCSVExitsOneWithMessageIntact pins that csvsrc's "file is
// empty" error surfaces to the user verbatim (exit 1, message on stderr),
// rather than being swallowed or rewritten by the CLI.
func TestRun_EmptyCSVExitsOneWithMessageIntact(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "empty.csv", []byte(""))

	stdout, stderr, code := runCLI(t, path)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stdout=%q stderr=%q)", code, stdout, stderr)
	}
	if !strings.HasPrefix(stderr, "exshell: ") {
		t.Errorf("stderr = %q, want it to start with %q", stderr, "exshell: ")
	}
	if !strings.Contains(stderr, "file is empty") {
		t.Errorf("stderr = %q, want it to contain csvsrc's \"file is empty\" message intact", stderr)
	}
}

// --- flag/positional ordering ---------------------------------------------
//
// flag.Parse alone stops at the first non-flag token, so a naive call
// would silently treat "exshell data.csv --list-sheets" as two file
// arguments instead of one file plus a flag — exactly the bug these tests
// guard against.

// TestRun_BooleanFlagAfterFile pins that a boolean flag (--list-sheets)
// placed after the file argument still takes effect, instead of being
// swallowed as a second positional.
func TestRun_BooleanFlagAfterFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "data.csv", []byte("a,b\n1,2\n"))

	stdout, stderr, code := runCLI(t, path, "--list-sheets")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr=%q)", code, stderr)
	}
	if stdout != "data.csv\n" {
		t.Errorf("stdout = %q, want %q", stdout, "data.csv\n")
	}
}

// TestRun_ValueTakingFlagAfterFile pins that a value-taking flag
// (--sheet NAME) placed after the file argument still takes effect.
func TestRun_ValueTakingFlagAfterFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.xlsx")
	buildXLSX(t, path, func(f *excelize.File) {
		if _, err := f.NewSheet("Data"); err != nil {
			t.Fatalf("NewSheet: %v", err)
		}
		f.SetCellValue("Data", "A1", "hello")
	})

	stdout, stderr, code := runCLI(t, path, "--sheet", "Data")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout, "hello") {
		t.Errorf("stdout = %q, want it to contain the Data sheet's content", stdout)
	}
}

// TestRun_DelimValueNotMistakenForFile pins that "--delim ';' file.csv"
// still parses ';' as --delim's value, not as an extra positional/the
// file — flag.Parse already knows --delim takes a value, so the fix for
// flags-after-file (which peels off exactly one positional per pass) must
// not disturb that.
func TestRun_DelimValueNotMistakenForFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "semi.csv", []byte("a;b;c\n1;2;3\n"))

	stdout, stderr, code := runCLI(t, "--delim", ";", path)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr=%q)", code, stderr)
	}
	want := "a  b  c\n-  -  -\n1  2  3\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestRun_OLE2LegacyXLSXExitsOneWithMessageIntact pins that xlsxsrc's OLE2
// container error — which deliberately covers both password-protection and
// legacy .xls as possibilities, since the header alone can't distinguish
// them — surfaces to the user verbatim (exit 1, message on stderr).
// --- C2: --max-col-width validation --------------------------------------

// TestRun_NegativeMaxColWidthExitsTwo pins C2's repro: a negative
// --max-col-width used to panic (strings.Repeat with a negative count)
// after already writing a garbage header line; it must instead be a clean
// usage error, with nothing written to stdout.
func TestRun_NegativeMaxColWidthExitsTwo(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "t.csv", []byte("a,b\n1,2\n"))

	stdout, stderr, code := runCLI(t, "--max-col-width", "-1", path)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (stdout=%q stderr=%q)", code, stdout, stderr)
	}
	if !strings.HasPrefix(stderr, "exshell: ") {
		t.Errorf("stderr = %q, want it to start with %q", stderr, "exshell: ")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (no garbage header line written before the error)", stdout)
	}
}

// TestRun_ExplicitZeroMaxColWidthExitsTwo pins that an explicit
// "--max-col-width 0" is rejected exactly like a negative value, not
// silently treated as "unset" — the flag package can't tell those apart by
// value alone, so Run must use fs.Visit to tell them apart.
func TestRun_ExplicitZeroMaxColWidthExitsTwo(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "t.csv", []byte("a,b\n1,2\n"))

	stdout, stderr, code := runCLI(t, "--max-col-width", "0", path)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (stdout=%q stderr=%q)", code, stdout, stderr)
	}
	if !strings.HasPrefix(stderr, "exshell: ") {
		t.Errorf("stderr = %q, want it to start with %q", stderr, "exshell: ")
	}
}

// TestRun_UnsetMaxColWidthStillDefaults pins that simply not passing
// --max-col-width at all (the value that also parses to 0) is not a usage
// error: it must still print normally, using layout's default cap.
func TestRun_UnsetMaxColWidthStillDefaults(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "t.csv", []byte("a,b\n1,2\n"))

	stdout, stderr, code := runCLI(t, path)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr=%q)", code, stderr)
	}
	if stdout == "" {
		t.Errorf("stdout empty, want the printed table")
	}
}

// TestRun_PositiveMaxColWidthStillWorks pins that a valid explicit cap is
// unaffected by the new validation.
func TestRun_PositiveMaxColWidthStillWorks(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "t.csv", []byte("label\nthis value is much longer than five cells\n"))

	stdout, stderr, code := runCLI(t, "--max-col-width", "5", path)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout, "this…") {
		t.Errorf("stdout = %q, want the value truncated to 5 cells", stdout)
	}
}

// --- Minor 4: an invalid --delim is a usage error, not a leaked "csv:"
// runtime error --------------------------------------------------------

func TestRun_InvalidDelimQuoteExitsTwo(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "t.csv", []byte("a,b\n1,2\n"))

	stdout, stderr, code := runCLI(t, "--delim", `"`, path)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (stdout=%q stderr=%q)", code, stdout, stderr)
	}
	if !strings.HasPrefix(stderr, "exshell: ") {
		t.Errorf("stderr = %q, want it to start with %q", stderr, "exshell: ")
	}
	if strings.Contains(stderr, "csv:") {
		t.Errorf("stderr = %q, want no leaked \"csv:\" runtime-error prefix", stderr)
	}
}

func TestRun_InvalidDelimCarriageReturnExitsTwo(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "t.csv", []byte("a,b\n1,2\n"))

	stdout, stderr, code := runCLI(t, "--delim", "\r", path)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (stdout=%q stderr=%q)", code, stdout, stderr)
	}
	if !strings.HasPrefix(stderr, "exshell: ") {
		t.Errorf("stderr = %q, want it to start with %q", stderr, "exshell: ")
	}
}

// --- I2: an empty xlsx sheet fails loudly instead of printing a blank grid
// at exit 0 --------------------------------------------------------------

// TestRun_EmptyXLSXSheetExitsOneWithMessage pins I2's repro: an xlsx sheet
// with zero columns used to print two blank lines and exit 0 —
// indistinguishable from success, and inconsistent with the CSV
// equivalent's exit 1 "file is empty".
func TestRun_EmptyXLSXSheetExitsOneWithMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.xlsx")
	buildXLSX(t, path, func(f *excelize.File) {
		if _, err := f.NewSheet("Empty"); err != nil {
			t.Fatalf("NewSheet: %v", err)
		}
	})

	stdout, stderr, code := runCLI(t, "--sheet", "Empty", path)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stdout=%q stderr=%q)", code, stdout, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (no blank-grid output)", stdout)
	}
	if !strings.HasPrefix(stderr, "exshell: ") {
		t.Errorf("stderr = %q, want it to start with %q", stderr, "exshell: ")
	}
	if !strings.Contains(stderr, "Empty") {
		t.Errorf("stderr = %q, want it to name the empty sheet", stderr)
	}
}

func TestRun_OLE2LegacyXLSXExitsOneWithMessageIntact(t *testing.T) {
	dir := t.TempDir()
	ole2Header := []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}
	data := append(ole2Header, make([]byte, 512)...)
	path := writeFile(t, dir, "legacy-or-encrypted.xlsx", data)

	stdout, stderr, code := runCLI(t, path)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stdout=%q stderr=%q)", code, stdout, stderr)
	}
	if !strings.HasPrefix(stderr, "exshell: ") {
		t.Errorf("stderr = %q, want it to start with %q", stderr, "exshell: ")
	}
	if !strings.Contains(stderr, "password") || !strings.Contains(stderr, ".xls") {
		t.Errorf("stderr = %q, want it to mention both password-protection and legacy .xls as possibilities", stderr)
	}
}
