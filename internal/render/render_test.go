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

// TestTable_NumericLastColumnEmptyNoTrailingWhitespace pins Minor 1's
// corrected fix (NEW-1): a numeric last column stays right-aligned via
// layout.Pad even when it is the last column — Ann's "500" must be padded
// out to the column's full width, not left flush against the separator —
// while an empty value on that same column (Bob's row) must still produce
// no trailing whitespace at all. The "no trailing whitespace" invariant is
// enforced once, robustly, by writeRow's whole-line right-trim, not by
// refusing to align numeric columns. No existing golden fixture had an
// empty final cell, so render_test.go's own
// assertNoTrailingWhitespaceOrMissingNewline never actually caught the
// original bug.
func TestTable_NumericLastColumnEmptyNoTrailingWhitespace(t *testing.T) {
	tbl := table.New("t.csv", []string{"name", "dept", "bonus"}, [][]string{
		{"Bob", "Sales", ""},
		{"Ann", "Eng", "500"},
	})
	var buf bytes.Buffer
	if err := Table(&buf, tbl, Options{Header: true}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	assertNoTrailingWhitespaceOrMissingNewline(t, buf.Bytes())

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	// bonus is numeric (500 is the only non-empty sample), width 5
	// (from the "bonus" header). Ann's "500" is right-aligned (2 leading
	// pad cells); Bob's empty value produces nothing at all, once the
	// whole line is right-trimmed.
	want := []string{"name  dept   bonus", "----  -----  -----", "Bob   Sales", "Ann   Eng      500"}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d: got %q, want %q", i, lines[i], w)
		}
	}
}

// TestTable_SecondToLastCellEmptyNoTrailingWhitespace pins NEW-1's other
// reported shape: an empty *non-last* cell's own left-aligned padding
// becomes trailing whitespace once the genuinely-last cell (also empty
// here) is trimmed away — a shape the original Minor 1 fix (which only
// ever looked at the last cell) could not reach at all.
func TestTable_SecondToLastCellEmptyNoTrailingWhitespace(t *testing.T) {
	tbl := table.New("t.csv", []string{"name", "dept", "bonus"}, [][]string{
		{"Bob", "", ""},
		{"Ann", "Eng", "500"},
	})
	var buf bytes.Buffer
	if err := Table(&buf, tbl, Options{Header: true}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	assertNoTrailingWhitespaceOrMissingNewline(t, buf.Bytes())

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if lines[2] != "Bob" {
		t.Errorf("line 2 = %q, want %q (dept and bonus both empty, trimmed away entirely)", lines[2], "Bob")
	}
}

// TestTable_AllEmptyRowNoTrailingWhitespace pins the most degenerate shape
// in NEW-1: every cell in a row is empty. The line must still end exactly
// at the newline, with no whitespace at all.
func TestTable_AllEmptyRowNoTrailingWhitespace(t *testing.T) {
	tbl := table.New("t.csv", []string{"a", "b", "c"}, [][]string{
		{"", "", ""},
		{"x", "y", "z"},
	})
	var buf bytes.Buffer
	if err := Table(&buf, tbl, Options{Header: true}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	assertNoTrailingWhitespaceOrMissingNewline(t, buf.Bytes())

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if lines[2] != "" {
		t.Errorf("all-empty data row = %q, want an empty line", lines[2])
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

// --- I1: unconstrained output must not lose bytes (R18) ---

// TestTable_UnconstrainedNoCapMeasuresBeyondSampleRows pins the CLI-level
// repro from the brief at unit-test speed: with Width==0 (unconstrained —
// piped/redirected) and no explicit MaxColWidth, a value past the default
// 1000-row sample window must still make it into the column width and
// therefore into the output, uncut, unlike the TTY-constrained path where
// sampling and the default cap both still apply.
func TestTable_UnconstrainedNoCapMeasuresBeyondSampleRows(t *testing.T) {
	const needle = "THIS_IS_A_VERY_LONG_VALUE_BEYOND_SAMPLE_WINDOW_AND_BEYOND_THE_DEFAULT_CAP_TOO"
	rows := make([][]string, 1200)
	for i := range rows {
		rows[i] = []string{"short"}
	}
	rows[1100] = []string{needle} // well past layout's default 1000-row sample
	tbl := table.New("t.csv", []string{"val"}, rows)

	var buf bytes.Buffer
	if err := Table(&buf, tbl, Options{Header: true, Width: 0}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	if !strings.Contains(buf.String(), needle) {
		t.Fatalf("unconstrained output lost/truncated a value beyond the sample window and default cap: needle not found")
	}
}

// TestTable_UnconstrainedExplicitCapStillTruncates pins the other half of
// I1's ruling: an explicitly user-supplied --max-col-width remains
// authoritative and still truncates, even on the unconstrained (piped)
// path that otherwise drops the cap entirely.
func TestTable_UnconstrainedExplicitCapStillTruncates(t *testing.T) {
	const needle = "THIS_VALUE_MUST_BE_TRUNCATED_AWAY_BY_AN_EXPLICIT_CAP"
	tbl := table.New("t.csv", []string{"val"}, [][]string{{needle}})

	var buf bytes.Buffer
	if err := Table(&buf, tbl, Options{Header: true, Width: 0, MaxColWidth: 10}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	if strings.Contains(buf.String(), needle) {
		t.Fatalf("explicit --max-col-width was not honored on the unconstrained path: got %q", buf.String())
	}
}

// TestTable_TTYConstrainedStillSamplesAndCaps pins that the sampling/cap
// behavior change is scoped to Width==0 only: a TTY-constrained print
// (Width > 0) must keep applying the default sample window and cap exactly
// as before I1, so a value beyond SampleRows still doesn't win the width
// war for a real terminal.
func TestTable_TTYConstrainedStillSamplesAndCaps(t *testing.T) {
	rows := make([][]string, 1200)
	for i := range rows {
		rows[i] = []string{"short"}
	}
	rows[1100] = []string{strings.Repeat("z", 100)} // beyond sample, beyond cap
	tbl := table.New("t.csv", []string{"val"}, rows)

	var buf bytes.Buffer
	if err := Table(&buf, tbl, Options{Header: true, Width: 80}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	if strings.Contains(buf.String(), strings.Repeat("z", 100)) {
		t.Fatalf("TTY-constrained path picked up a value beyond SampleRows/MaxColWidth: %q", buf.String())
	}
}

// --- R19 (NEW-2): the unconstrained no-cap path must not let one outlier
// cell inflate every other row's padding ---

// TestTable_UnconstrainedLongValueSurvivesWithoutInflatingOtherRows pins
// both halves of R19 at once: a single very long value in an otherwise
// narrow column must (a) still come through completely intact — R18's
// no-data-loss guarantee, unaffected by R19 — and (b) not force every
// other (short) row's rendering to widen to match it. Without the R19
// bound, every one of the 999 short rows would be padded out to the
// outlier's width; with it, only the outlier row's line is long.
//
// The outlier deliberately sits in "message", the *first* (non-last)
// column, with a trivial "status" column after it: the last column is
// always Truncate-only (see formatRow) and was never vulnerable to
// cross-row padding inflation even before R19, so putting the outlier
// there would test nothing — the amplification bug lives specifically in
// the Pad path a non-last (or numeric-last) column takes.
func TestTable_UnconstrainedLongValueSurvivesWithoutInflatingOtherRows(t *testing.T) {
	long := strings.Repeat("x", 5000)

	rows := make([][]string, 1000)
	for i := range rows {
		rows[i] = []string{"short", "ok"}
	}
	rows[500] = []string{long, "ok"}
	tbl := table.New("t.csv", []string{"message", "status"}, rows)

	var buf bytes.Buffer
	if err := Table(&buf, tbl, Options{Header: true, Width: 0}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	out := buf.String()

	// (a) content survives, untruncated and grep-able.
	if !strings.Contains(out, long) {
		t.Fatalf("the 5000-char outlier value did not survive intact")
	}

	// (b) an ordinary short row is not inflated to match the outlier: its
	// line length must stay in the same ballpark as its own content, not
	// balloon to ~5000+ characters.
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	for i, line := range lines {
		if strings.Contains(line, long) {
			continue // the outlier row itself is expected to be long
		}
		if len(line) > 100 {
			t.Fatalf("line %d (not the outlier row) is %d bytes, want well under 100 — an ordinary row was inflated to match the outlier: %q", i, len(line), line)
		}
	}
}

// TestTable_UnconstrainedOutputStaysProportionateToInput pins the
// amplification regression the re-reviewer measured directly: 51,800 bytes
// in producing 40,086,929 bytes out (774x) on a real shape. This
// reconstructs that shape at a smaller but still decisive scale — many
// short rows plus one long outlier, in a non-last column (see the comment
// on TestTable_UnconstrainedLongValueSurvivesWithoutInflatingOtherRows for
// why that placement matters) — and asserts total output stays within a
// small constant multiple of input size, not proportional to (row count) x
// (outlier width).
func TestTable_UnconstrainedOutputStaysProportionateToInput(t *testing.T) {
	const nRows = 2000
	const outlierWidth = 2000

	rows := make([][]string, nRows)
	inputBytes := 0
	for i := range rows {
		rows[i] = []string{"short-value", "z"}
		inputBytes += len(rows[i][0]) + len(rows[i][1]) + 1 // +1 for a CSV newline, approximately
	}
	rows[nRows/2] = []string{strings.Repeat("y", outlierWidth), "z"}
	inputBytes += outlierWidth
	tbl := table.New("t.csv", []string{"col", "tail"}, rows)

	var buf bytes.Buffer
	if err := Table(&buf, tbl, Options{Header: true, Width: 0}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	outputBytes := buf.Len()

	// Without the R19 bound, every one of nRows rows pads "col" to
	// ~outlierWidth, so output would be roughly nRows*outlierWidth ~=
	// 4,000,000 bytes here. With the bound, output is roughly
	// nRows*padWidthBound plus the one outlier row's real length:
	// comfortably under nRows*100.
	if maxReasonable := nRows * 100; outputBytes > maxReasonable {
		t.Fatalf("output is %d bytes for %d bytes of input (nRows=%d, one outlier of width %d) — want under %d; padding was not bounded",
			outputBytes, inputBytes, nRows, outlierWidth, maxReasonable)
	}
	if !strings.Contains(buf.String(), strings.Repeat("y", outlierWidth)) {
		t.Fatalf("the outlier value did not survive intact")
	}
}

// --- C3: cell sanitization ---

// TestTable_SanitizesEmbeddedNewlineKeepsOneLinePerRow pins that a quoted
// CSV field's embedded newline (which csvsrc correctly preserves in the
// data model — see csvsrc_test.go) is neutralized before rendering, so one
// logical row is always exactly one physical output line.
func TestTable_SanitizesEmbeddedNewlineKeepsOneLinePerRow(t *testing.T) {
	tbl := table.New("t.csv", []string{"a", "b", "c"}, [][]string{
		{"1", "line one\nline two", "3"},
		{"4", "five", "6"},
	})
	var buf bytes.Buffer
	if err := Table(&buf, tbl, Options{Header: true}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 4 { // header, rule, 2 data rows
		t.Fatalf("got %d physical lines, want 4 (one per logical row plus header/rule): %q", len(lines), buf.String())
	}
	if !strings.Contains(lines[2], "line one line two") {
		t.Fatalf("data line = %q, want the embedded newline collapsed to a space", lines[2])
	}
}

// TestTable_SanitizesEscapeSequence pins that a raw ESC byte embedded in a
// cell value (e.g. a terminal title-setting escape sequence smuggled into a
// CSV cell) never reaches the writer verbatim.
func TestTable_SanitizesEscapeSequence(t *testing.T) {
	tbl := table.New("t.csv", []string{"a", "b"}, [][]string{
		{"1", "\x1b]0;PWNED\a"},
	})
	var buf bytes.Buffer
	if err := Table(&buf, tbl, Options{Header: true}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	if strings.ContainsRune(buf.String(), 0x1b) || strings.ContainsRune(buf.String(), 0x07) {
		t.Fatalf("output contains a raw control byte: %q", buf.String())
	}
}
