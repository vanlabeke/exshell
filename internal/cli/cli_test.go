package cli

import (
	"bytes"
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

func TestRun_ForceInteractiveSurfacesNotImplemented(t *testing.T) {
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

// TestRun_OLE2LegacyXLSXExitsOneWithMessageIntact pins that xlsxsrc's OLE2
// container error — which deliberately covers both password-protection and
// legacy .xls as possibilities, since the header alone can't distinguish
// them — surfaces to the user verbatim (exit 1, message on stderr).
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
