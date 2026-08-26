package layout

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"vanlabeke.dev/exshell/internal/table"
)

// --- Compute: natural widths ---

func TestComputeNaturalWidth_HeaderWiderThanCells(t *testing.T) {
	tbl := table.New("t", []string{"Description"}, [][]string{
		{"x"},
		{"y"},
	})
	l := Compute(tbl, Options{})
	if got := l.Cols[0].Width; got != len("Description") {
		t.Fatalf("width = %d, want %d (header should win)", got, len("Description"))
	}
}

func TestComputeNaturalWidth_CellsWiderThanHeader(t *testing.T) {
	tbl := table.New("t", []string{"ID"}, [][]string{
		{"1"},
		{"999999"},
	})
	l := Compute(tbl, Options{})
	if got := l.Cols[0].Width; got != len("999999") {
		t.Fatalf("width = %d, want %d (data should win)", got, len("999999"))
	}
}

func TestComputeNaturalWidth_TwoColumns(t *testing.T) {
	tbl := table.New("t", []string{"ID", "Name"}, [][]string{
		{"1", "Bob"},
		{"22", "Alexandria"},
	})
	l := Compute(tbl, Options{})
	if l.Cols[0].Width != 2 {
		t.Errorf("col0 width = %d, want 2", l.Cols[0].Width)
	}
	if l.Cols[1].Width != 10 {
		t.Errorf("col1 width = %d, want 10 (len of Alexandria)", l.Cols[1].Width)
	}
}

// --- Compute: MaxColWidth clamp ---

func TestComputeMaxColWidthClamp(t *testing.T) {
	long := strings.Repeat("x", 100)
	tbl := table.New("t", []string{"Col"}, [][]string{
		{long},
	})
	l := Compute(tbl, Options{MaxColWidth: 5})
	if l.Cols[0].Width != 5 {
		t.Fatalf("width = %d, want clamped to 5", l.Cols[0].Width)
	}
}

func TestComputeMaxColWidthDefault(t *testing.T) {
	long := strings.Repeat("x", 100)
	tbl := table.New("t", []string{"Col"}, [][]string{
		{long},
	})
	l := Compute(tbl, Options{})
	if l.Cols[0].Width != 40 {
		t.Fatalf("width = %d, want default clamp of 40", l.Cols[0].Width)
	}
}

// --- Compute: SampleRows bound ---

func TestComputeSampleRowsBound(t *testing.T) {
	wide := strings.Repeat("z", 50)
	tbl := table.New("t", []string{"Col"}, [][]string{
		{"a"},
		{"b"},
		{wide}, // beyond SampleRows: 2, must not affect width
		{"c"},
	})
	l := Compute(tbl, Options{SampleRows: 2})
	if l.Cols[0].Width != len("Col") {
		t.Fatalf("width = %d, want %d (only header + first 2 rows sampled)", l.Cols[0].Width, len("Col"))
	}
}

// --- Compute: fit algorithm determinism & tie-break ---

func TestComputeFitDeterministicTieBreak(t *testing.T) {
	// Two columns, both natural width 10 (via 10-char headers, no data).
	tbl := table.New("t", []string{"AAAAAAAAAA", "BBBBBBBBBB"}, [][]string{})
	l := Compute(tbl, Options{TotalWidth: 15})
	// Traced by hand: widths shrink 10,10 -> ... -> 7,8 (lowest index shrinks
	// first on every tie), summing to exactly 15.
	if l.Cols[0].Width != 7 {
		t.Errorf("col0 width = %d, want 7", l.Cols[0].Width)
	}
	if l.Cols[1].Width != 8 {
		t.Errorf("col1 width = %d, want 8", l.Cols[1].Width)
	}
	sum := l.Cols[0].Width + l.Cols[1].Width
	if sum != 15 {
		t.Errorf("sum = %d, want 15", sum)
	}
}

// --- Compute: MinColWidth floor ---

func TestComputeMinColWidthFloor(t *testing.T) {
	c20 := strings.Repeat("x", 20)
	tbl := table.New("t", []string{"A", "B", "C"}, [][]string{
		{c20, c20, c20},
	})
	l := Compute(tbl, Options{TotalWidth: 5, MinColWidth: 6})
	for i, c := range l.Cols {
		if c.Width != 6 {
			t.Errorf("col%d width = %d, want floored to 6", i, c.Width)
		}
		if c.Width < 0 {
			t.Errorf("col%d width negative", i)
		}
	}
}

func TestComputeMinColWidthDefault(t *testing.T) {
	c20 := strings.Repeat("x", 20)
	tbl := table.New("t", []string{"A", "B", "C"}, [][]string{
		{c20, c20, c20},
	})
	l := Compute(tbl, Options{TotalWidth: 1})
	for i, c := range l.Cols {
		if c.Width != 6 {
			t.Errorf("col%d width = %d, want floored to default MinColWidth 6", i, c.Width)
		}
	}
}

// --- Compute: TotalWidth 0 unconstrained ---

func TestComputeTotalWidthZeroUnconstrained(t *testing.T) {
	tbl := table.New("t", []string{"ID", "Name"}, [][]string{
		{"1", "Bob"},
		{"22", "Alexandria"},
	})
	l := Compute(tbl, Options{TotalWidth: 0})
	if l.Cols[0].Width != 2 {
		t.Errorf("col0 width = %d, want 2 (unconstrained natural width)", l.Cols[0].Width)
	}
	if l.Cols[1].Width != 10 {
		t.Errorf("col1 width = %d, want 10 (unconstrained natural width)", l.Cols[1].Width)
	}
}

// --- Truncate ---

func TestTruncateZeroWidth(t *testing.T) {
	if got := Truncate("hello", 0); got != "" {
		t.Fatalf("Truncate(_, 0) = %q, want empty", got)
	}
}

func TestTruncateNegativeWidth(t *testing.T) {
	if got := Truncate("hello", -3); got != "" {
		t.Fatalf("Truncate(_, -3) = %q, want empty", got)
	}
}

func TestTruncateWidthOne(t *testing.T) {
	got := Truncate("hello world", 1)
	if got != "…" {
		t.Fatalf("Truncate(_, 1) = %q, want %q", got, "…")
	}
	if runewidth.StringWidth(got) != 1 {
		t.Fatalf("width = %d, want 1", runewidth.StringWidth(got))
	}
}

func TestTruncateNoTrimNeeded(t *testing.T) {
	// Exactly at the boundary: string width equals w, no trimming, no ellipsis.
	s := "hello"
	got := Truncate(s, runewidth.StringWidth(s))
	if got != s {
		t.Fatalf("Truncate at exact width = %q, want unchanged %q", got, s)
	}
}

func TestTruncateUnderBudgetNoTrim(t *testing.T) {
	got := Truncate("hi", 10)
	if got != "hi" {
		t.Fatalf("Truncate with budget to spare = %q, want unchanged %q", got, "hi")
	}
}

func TestTruncateCJK(t *testing.T) {
	s := "日本語テスト" // 6 runes, width 2 each = 12
	for _, w := range []int{2, 3, 4, 5, 11} {
		got := Truncate(s, w)
		if runewidth.StringWidth(got) != w {
			t.Fatalf("Truncate(%q, %d) = %q width %d, want %d", s, w, got, runewidth.StringWidth(got), w)
		}
		if !strings.HasSuffix(got, "…") {
			t.Fatalf("Truncate(%q, %d) = %q, want ellipsis suffix", s, w, got)
		}
	}
}

func TestTruncateEmoji(t *testing.T) {
	s := "😀😀😀😀😀 rest of string here"
	full := runewidth.StringWidth(s)
	for _, w := range []int{1, 2, 3, 5, 8} {
		if w >= full {
			continue
		}
		got := Truncate(s, w)
		if runewidth.StringWidth(got) != w {
			t.Fatalf("Truncate(%q, %d) = %q width %d, want %d", s, w, got, runewidth.StringWidth(got), w)
		}
	}
}

func TestTruncateWideRuneSpareCellPadded(t *testing.T) {
	// A narrow char followed by a wide one: at some width the wide rune
	// cannot fit and must be dropped whole, potentially leaving a spare cell
	// that must be padded rather than left short.
	s := "a" + "日" // width 1 + 2 = 3
	got := Truncate(s, 2)
	if runewidth.StringWidth(got) != 2 {
		t.Fatalf("Truncate(%q, 2) = %q width %d, want 2", s, got, runewidth.StringWidth(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("Truncate(%q, 2) = %q, want ellipsis suffix", s, got)
	}
}

// --- Pad ---

func TestPadLeftAlign(t *testing.T) {
	got := Pad("hi", 5, false)
	if got != "hi   " {
		t.Fatalf("Pad = %q, want %q", got, "hi   ")
	}
	if runewidth.StringWidth(got) != 5 {
		t.Fatalf("width = %d, want 5", runewidth.StringWidth(got))
	}
}

func TestPadRightAlign(t *testing.T) {
	got := Pad("hi", 5, true)
	if got != "   hi" {
		t.Fatalf("Pad = %q, want %q", got, "   hi")
	}
}

func TestPadExactWidth(t *testing.T) {
	got := Pad("hello", 5, false)
	if got != "hello" {
		t.Fatalf("Pad = %q, want unchanged %q", got, "hello")
	}
}

func TestPadOverLongInputTruncated(t *testing.T) {
	got := Pad("hello world this is long", 5, false)
	if runewidth.StringWidth(got) != 5 {
		t.Fatalf("Pad width = %d, want 5 (never overflow)", runewidth.StringWidth(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("Pad over-long = %q, want truncated with ellipsis", got)
	}
}

func TestPadOverLongInputTruncatedRightAlign(t *testing.T) {
	got := Pad("hello world this is long", 5, true)
	if runewidth.StringWidth(got) != 5 {
		t.Fatalf("Pad width = %d, want 5 (never overflow)", runewidth.StringWidth(got))
	}
}

func TestPadCJKAlignment(t *testing.T) {
	got := Pad("日本", 6, false) // width 4, pad 2 spaces on right
	if runewidth.StringWidth(got) != 6 {
		t.Fatalf("width = %d, want 6", runewidth.StringWidth(got))
	}
	if !strings.HasPrefix(got, "日本") {
		t.Fatalf("Pad(_, _, false) = %q, want prefix 日本", got)
	}
}

// --- Numeric detection ---

func TestNumericDetection_ExactlyEightyPercent(t *testing.T) {
	rows := make([][]string, 100)
	for i := 0; i < 80; i++ {
		rows[i] = []string{"1"}
	}
	for i := 80; i < 100; i++ {
		rows[i] = []string{"abc"}
	}
	tbl := table.New("t", []string{"Col"}, rows)
	l := Compute(tbl, Options{SampleRows: 100})
	if !l.Cols[0].Numeric {
		t.Fatalf("Numeric = false at exactly 80%%, want true")
	}
}

func TestNumericDetection_JustUnderEightyPercent(t *testing.T) {
	rows := make([][]string, 100)
	for i := 0; i < 79; i++ {
		rows[i] = []string{"1"}
	}
	for i := 79; i < 100; i++ {
		rows[i] = []string{"abc"}
	}
	tbl := table.New("t", []string{"Col"}, rows)
	l := Compute(tbl, Options{SampleRows: 100})
	if l.Cols[0].Numeric {
		t.Fatalf("Numeric = true at 79%%, want false (just under 80%%)")
	}
}

func TestNumericDetection_FormattedNumbers(t *testing.T) {
	tbl := table.New("t", []string{"Col"}, [][]string{
		{"1.234,56"},
		{"$1,200"},
		{"45%"},
	})
	l := Compute(tbl, Options{})
	if !l.Cols[0].Numeric {
		t.Fatalf("Numeric = false for formatted numbers, want true")
	}
}

func TestNumericDetection_EmptyColumnNotNumeric(t *testing.T) {
	tbl := table.New("t", []string{"Col"}, [][]string{
		{""},
		{""},
		{""},
	})
	l := Compute(tbl, Options{})
	if l.Cols[0].Numeric {
		t.Fatalf("Numeric = true for all-empty column, want false")
	}
}

func TestNumericDetection_EmptyCellsExcludedFromDenominator(t *testing.T) {
	tbl := table.New("t", []string{"Col"}, [][]string{
		{"1"},
		{"2"},
		{""}, // excluded
		{""}, // excluded
	})
	l := Compute(tbl, Options{})
	if !l.Cols[0].Numeric {
		t.Fatalf("Numeric = false, want true (2/2 non-empty parse, empties excluded)")
	}
}

func TestNumericDetection_NonNumericColumn(t *testing.T) {
	tbl := table.New("t", []string{"Col"}, [][]string{
		{"apple"},
		{"banana"},
	})
	l := Compute(tbl, Options{})
	if l.Cols[0].Numeric {
		t.Fatalf("Numeric = true for text column, want false")
	}
}
