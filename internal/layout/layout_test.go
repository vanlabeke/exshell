package layout

import (
	"strings"
	"testing"
	"time"

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

// --- Compute: Width>=1 invariant (C2) ---
//
// Compute must guarantee Col.Width >= 1 regardless of what MaxColWidth it is
// given, even a value the CLI should already have rejected as a usage
// error: layout owns this invariant defensively rather than trusting every
// caller to validate first, per the brief's ruling that render.go's
// strings.Repeat panic was ultimately layout's invariant to keep.

func TestComputeWidthInvariant_NegativeMaxColWidthNeverProducesNegativeWidth(t *testing.T) {
	tbl := table.New("t", []string{"label"}, [][]string{
		{"this value is much longer than five cells"},
	})
	l := Compute(tbl, Options{MaxColWidth: -1})
	if l.Cols[0].Width != 1 {
		t.Fatalf("width = %d, want floored to 1 for MaxColWidth -1", l.Cols[0].Width)
	}
}

func TestComputeWidthInvariant_ZeroWidthColumnNeverProducesNegativeWidth(t *testing.T) {
	// An empty header and no rows naturally measures 0 before the floor.
	tbl := table.New("t", []string{""}, nil)
	l := Compute(tbl, Options{MaxColWidth: -5})
	if l.Cols[0].Width != 1 {
		t.Fatalf("width = %d, want floored to 1", l.Cols[0].Width)
	}
}

// --- Compute/render: Unbounded sentinel (I1) ---

func TestComputeUnboundedMaxColWidthDisablesCap(t *testing.T) {
	long := strings.Repeat("x", 500)
	tbl := table.New("t", []string{"Col"}, [][]string{{long}})
	l := Compute(tbl, Options{MaxColWidth: Unbounded})
	if l.Cols[0].Width != 500 {
		t.Fatalf("width = %d, want 500 (uncapped)", l.Cols[0].Width)
	}
}

func TestComputeUnboundedSampleRowsMeasuresEveryRow(t *testing.T) {
	rows := make([][]string, 1500)
	for i := range rows {
		rows[i] = []string{"a"}
	}
	rows[1200] = []string{strings.Repeat("z", 50)} // beyond the default 1000-row sample
	tbl := table.New("t", []string{"Col"}, rows)

	l := Compute(tbl, Options{SampleRows: Unbounded, MaxColWidth: Unbounded})
	if l.Cols[0].Width != 50 {
		t.Fatalf("width = %d, want 50 (row 1200 must be measured)", l.Cols[0].Width)
	}
}

// --- Compute: PadWidth (R19) ---

// TestComputePadWidthEqualsWidthOutsideUnbounded pins that PadWidth is
// never a behavior change for any mode except MaxColWidth: Unbounded —
// every other caller (the default cap, an explicit cap, or a TotalWidth
// fit) can keep reading Width as before and PadWidth will always agree.
func TestComputePadWidthEqualsWidthOutsideUnbounded(t *testing.T) {
	long := strings.Repeat("x", 100)
	tbl := table.New("t", []string{"Col"}, [][]string{{long}})

	cases := []Options{
		{},                // default cap
		{MaxColWidth: 10}, // explicit cap
		{TotalWidth: 5},   // fit()
	}
	for _, opts := range cases {
		l := Compute(tbl, opts)
		if l.Cols[0].Width != l.Cols[0].PadWidth {
			t.Errorf("opts=%+v: Width=%d, PadWidth=%d, want equal", opts, l.Cols[0].Width, l.Cols[0].PadWidth)
		}
	}
}

// TestComputePadWidthBoundedUnderUnbounded pins R19 itself: with
// MaxColWidth: Unbounded, a column's Width reflects the true (uncapped)
// natural width, but PadWidth is capped at padWidthBound (40) regardless —
// this is the field render.Table's no-cap path uses to pad ordinary rows,
// so a single outlier value can't inflate every row's padding to match it.
func TestComputePadWidthBoundedUnderUnbounded(t *testing.T) {
	long := strings.Repeat("x", 5000)
	tbl := table.New("t", []string{"Col"}, [][]string{{long}})

	l := Compute(tbl, Options{MaxColWidth: Unbounded})
	if l.Cols[0].Width != 5000 {
		t.Fatalf("Width = %d, want 5000 (uncapped, no data loss)", l.Cols[0].Width)
	}
	if l.Cols[0].PadWidth != 40 {
		t.Fatalf("PadWidth = %d, want 40 (bounded)", l.Cols[0].PadWidth)
	}
}

// TestComputePadWidthNotBoundedWhenWithinBound pins that PadWidth doesn't
// artificially shrink a column that's already narrower than the bound —
// R19 only ever caps down, never pads a short natural width up.
func TestComputePadWidthNotBoundedWhenWithinBound(t *testing.T) {
	tbl := table.New("t", []string{"Col"}, [][]string{{"short"}})
	l := Compute(tbl, Options{MaxColWidth: Unbounded})
	if l.Cols[0].PadWidth != l.Cols[0].Width {
		t.Fatalf("PadWidth = %d, Width = %d, want equal for a column under the bound", l.Cols[0].PadWidth, l.Cols[0].Width)
	}
}

// TestComputeUnboundedWithTotalWidthDoesNotHang is a regression test for a
// latent (not currently reachable) bug the re-reviewer flagged: pairing
// MaxColWidth: Unbounded with a finite TotalWidth would send fit()
// decrementing one cell at a time from math.MaxInt — effectively an
// infinite loop — without Compute's defensive pre-clamp. Run with a hard
// timeout so a regression here fails the test instead of hanging the whole
// suite.
func TestComputeUnboundedWithTotalWidthDoesNotHang(t *testing.T) {
	tbl := table.New("t", []string{"A", "B"}, [][]string{{"x", "y"}})

	done := make(chan Layout, 1)
	go func() {
		done <- Compute(tbl, Options{MaxColWidth: Unbounded, TotalWidth: 10})
	}()

	select {
	case l := <-done:
		sum := 0
		for _, c := range l.Cols {
			sum += c.Width
		}
		if sum > 10 {
			t.Errorf("sum of widths = %d, want <= TotalWidth (10)", sum)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Compute did not return within 2s — MaxColWidth: Unbounded paired with a finite TotalWidth is looping")
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
	// "a" + wide "日" + "b" at w=3 (target=2): "a" fills 1 of the 2 content
	// cells, then "日" (width 2) cannot fit in the remaining 1 cell and is
	// dropped whole, leaving a genuine spare cell in the content budget
	// (unlike "a"+"日" at w=2, target=1, where "a" already exactly fills the
	// budget and there is no spare either way). The spare must be padded
	// *before* the ellipsis so the ellipsis remains the trailing character:
	// want "a …", not "a… " (which is what you get if the ellipsis is
	// appended first and the spare padded afterward).
	s := "a" + "日" + "b" // width 1 + 2 + 1 = 4
	got := Truncate(s, 3)
	want := "a …"
	if got != want {
		t.Fatalf("Truncate(%q, 3) = %q, want %q", s, got, want)
	}
	if runewidth.StringWidth(got) != 3 {
		t.Fatalf("Truncate(%q, 3) = %q width %d, want 3", s, got, runewidth.StringWidth(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("Truncate(%q, 3) = %q, want ellipsis suffix", s, got)
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

// --- Sep / SepCost ---

func TestSepCost(t *testing.T) {
	cases := []struct {
		n    int
		want int
	}{
		{0, 0},
		{1, 0},
		{2, 2},
		{5, 8},
	}
	for _, c := range cases {
		if got := SepCost(c.n); got != c.want {
			t.Errorf("SepCost(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}

// --- Sanitize (C3) ---

func TestSanitize_NewlineCarriageReturnTabBecomeSpace(t *testing.T) {
	got := Sanitize("line1\nline2\r\ncol\ttab")
	want := "line1 line2  col tab"
	if got != want {
		t.Fatalf("Sanitize = %q, want %q", got, want)
	}
}

func TestSanitize_EscapeSequenceNeutralized(t *testing.T) {
	// ESC ]0;PWNED BEL — a terminal title-setting escape sequence. Neither
	// the ESC (0x1B) nor the BEL (0x07) may survive verbatim.
	raw := "\x1b]0;PWNED\a"
	got := Sanitize(raw)
	if strings.ContainsRune(got, 0x1b) || strings.ContainsRune(got, 0x07) {
		t.Fatalf("Sanitize(%q) = %q, still contains a raw control byte", raw, got)
	}
	// The literal text in between must survive; only the control bytes are
	// replaced.
	if !strings.Contains(got, "]0;PWNED") {
		t.Fatalf("Sanitize(%q) = %q, lost the non-control payload", raw, got)
	}
}

func TestSanitize_C1ControlReplaced(t *testing.T) {
	got := Sanitize("ab") // U+0085 NEL, a C1 control character
	if strings.ContainsRune(got, 0x85) {
		t.Fatalf("Sanitize = %q, still contains the C1 control character", got)
	}
	if !strings.HasPrefix(got, "a") || !strings.HasSuffix(got, "b") {
		t.Fatalf("Sanitize = %q, want surrounding text preserved", got)
	}
}

func TestSanitize_PlainTextUnchanged(t *testing.T) {
	s := "Alice, 東京太郎, Bob 🎉 — all clean"
	if got := Sanitize(s); got != s {
		t.Fatalf("Sanitize(%q) = %q, want unchanged", s, got)
	}
}

func TestSanitize_EmptyStringUnchanged(t *testing.T) {
	if got := Sanitize(""); got != "" {
		t.Fatalf("Sanitize(\"\") = %q, want empty", got)
	}
}

// TestComputeSanitizesBeforeMeasuring pins that Compute measures the
// sanitized form of a cell, not the raw one: runewidth scores \n and \t at
// ~0 cells, so a column whose only content is a newline/tab-bearing value
// would otherwise measure far too narrow — exactly what let C3's escape
// passthrough go unnoticed by width math for so long.
func TestComputeSanitizesBeforeMeasuring(t *testing.T) {
	tbl := table.New("t", []string{"Col"}, [][]string{
		{"line one\nline two"}, // sanitizes to "line one line two", width 17
	})
	l := Compute(tbl, Options{})
	want := len("line one line two")
	if l.Cols[0].Width != want {
		t.Fatalf("width = %d, want %d (measured on the sanitized form)", l.Cols[0].Width, want)
	}
}
