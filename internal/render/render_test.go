package render

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vanlabeke.dev/exshell/internal/table"
)

// update regenerates golden files when run as:
//
//	go test ./internal/render/... -run TestTable_Golden -update
var update = flag.Bool("update", false, "update golden files")

// wideFixture exercises numeric right-alignment (id), a CJK cell and an
// emoji cell in the same column (name, to pin width-correct truncation),
// a mid-length text column (city), a longer text column (email), and a
// column long enough to hit layout's default 40-cell clamp (bio) which is
// also the last column, to pin the "no trailing padding" rule.
func wideFixture() table.Table {
	cols := []string{"id", "name", "city", "email", "bio"}
	rows := [][]string{
		{"1", "Alice", "San Francisco, CA", "alice.wonderland@example.com", "Loves gardening and long walks on the beach every single weekend without fail"},
		{"2", "東京太郎", "Tokyo, Japan", "taro@example.co.jp", "N/A"},
		{"3", "Bob 🎉", "New York City, NY", "bob@example.com", "Short bio"},
	}
	return table.New("wide.csv", cols, rows)
}

func headerOnlyFixture() table.Table {
	return table.New("empty.csv", []string{"a", "b", "c"}, nil)
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)

	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden mismatch for %s:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// assertNoTrailingWhitespaceOrMissingNewline pins two format rules from the
// brief: every line ends with \n, and no line carries trailing spaces after
// its last visible column.
func assertNoTrailingWhitespaceOrMissingNewline(t *testing.T, out []byte) {
	t.Helper()
	if len(out) == 0 {
		return
	}
	if out[len(out)-1] != '\n' {
		t.Errorf("output does not end with a newline: %q", out)
	}
	for i, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if strings.HasSuffix(line, " ") {
			t.Errorf("line %d has trailing whitespace: %q", i, line)
		}
	}
}

func TestTable_Golden(t *testing.T) {
	cases := []struct {
		name   string
		tbl    table.Table
		opts   Options
		golden string
	}{
		{"width40", wideFixture(), Options{Width: 40, Header: true}, "wide_w40.golden"},
		{"width80", wideFixture(), Options{Width: 80, Header: true}, "wide_w80.golden"},
		{"width200", wideFixture(), Options{Width: 200, Header: true}, "wide_w200.golden"},
		{"unconstrained", wideFixture(), Options{Width: 0, Header: true}, "wide_w0.golden"},
		{"headerOnlyZeroRows", headerOnlyFixture(), Options{Width: 0, Header: true}, "header_only.golden"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Table(&buf, c.tbl, c.opts); err != nil {
				t.Fatalf("Table: %v", err)
			}
			assertNoTrailingWhitespaceOrMissingNewline(t, buf.Bytes())
			checkGolden(t, c.golden, buf.Bytes())
		})
	}
}

func TestTable_HeaderOnlyHasNoDataLines(t *testing.T) {
	var buf bytes.Buffer
	if err := Table(&buf, headerOnlyFixture(), Options{Header: true}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected exactly 2 lines (header + rule) for a zero-row table, got %d: %q", len(lines), buf.String())
	}
}

func TestTable_HeaderFalseOmitsHeaderAndRule(t *testing.T) {
	var buf bytes.Buffer
	if err := Table(&buf, wideFixture(), Options{Header: false}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	if strings.Contains(buf.String(), "----") {
		t.Errorf("expected no rule line when Header is false, got: %q", buf.String())
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 data lines, got %d: %q", len(lines), buf.String())
	}
}

func TestTable_MaxRowsLimitsDataNotHeader(t *testing.T) {
	var buf bytes.Buffer
	if err := Table(&buf, wideFixture(), Options{Header: true, MaxRows: 1}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	// header + rule + exactly one data row.
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header, rule, 1 data row), got %d: %q", len(lines), buf.String())
	}
}

func TestTable_NumericColumnRightAligned(t *testing.T) {
	tbl := table.New("t.csv", []string{"n", "label"}, [][]string{
		{"1", "x"},
		{"22", "y"},
	})
	var buf bytes.Buffer
	if err := Table(&buf, tbl, Options{Header: true}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	// n column width should be 2 (from "22"), right-aligned so "1" is
	// preceded by a space; "label" is the last column so it is never
	// padded.
	want := []string{" n  label", "--  -----", " 1  x", "22  y"}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d: got %q, want %q", i, lines[i], w)
		}
	}
}

func TestTable_MaxColWidthOverride(t *testing.T) {
	tbl := table.New("t.csv", []string{"label"}, [][]string{
		{"this value is much longer than five cells"},
	})
	var buf bytes.Buffer
	if err := Table(&buf, tbl, Options{Header: true, MaxColWidth: 5}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	// label is the only (and therefore last) column, non-numeric, so no
	// trailing pad is applied — but it must still be truncated to 5 cells.
	if lines[0] != "label" {
		t.Errorf("header: got %q", lines[0])
	}
	if lines[1] != "-----" {
		t.Errorf("rule: got %q", lines[1])
	}
	if lines[2] != "this…" {
		t.Errorf("data: got %q", lines[2])
	}
}
