# exshell — implementation spec

## Context

Reading tabular data in a terminal is awkward today. `cat` on a CSV gives
unaligned comma soup; `column -t` breaks on quoted fields containing the
delimiter; xlsx cannot be read at all without leaving the terminal for Excel or
LibreOffice. The gap is worst in the two most common real cases: an xlsx export
you want to glance at, and a semicolon-delimited CSV from a European Excel
install.

`exshell` ("excel" + "shell") closes that gap and nothing more. It is a
**viewer**, not a spreadsheet: no editing, no formulas, no sorting, no
filtering. Success means `exshell somefile.xlsx` shows readable, aligned,
correctly-typed data in under a second, and `exshell somefile.csv | grep`
behaves like any other Unix tool.

`/Users/cvanlabe/Development/vanlabeke.dev/exshell` is empty and is not yet a
git repo. This is a greenfield build.

## Settled decisions

| Decision | Choice |
|---|---|
| Language | Go, `go 1.26.0` in go.mod |
| Module path | `vanlabeke.dev/exshell` |
| Output modes | Print to stdout by default; viewer when stdout is a TTY **and** data exceeds the window |
| Viewer features | Scroll, search, cell inspect, sheet switching. **No sort, no filter, no edit.** |
| Sheets | First visible sheet by default; `--sheet` / `--list-sheets` **plus** Tab switching in the viewer |
| CSV dialect | Sniff delimiter + BOM, row 1 is header; `--delim` / `--no-header` / `--encoding` override |
| Scale | In-memory now, behind a `Table` interface so an indexed backend can replace it later |
| TUI stack | Bubble Tea v2 |
| Implementation | Sonnet subagents, one owner per package, waves below |

## Dependencies (verified available)

```
github.com/charmbracelet/bubbletea/v2 v2.0.9
github.com/charmbracelet/lipgloss/v2  v2.0.6
github.com/xuri/excelize/v2           v2.11.0
github.com/mattn/go-runewidth         v0.0.28
golang.org/x/text                     v0.41.0
golang.org/x/term                     latest
```

Local toolchain is go1.25.3 with `GOTOOLCHAIN=auto`, and
`golang.org/toolchain v0.0.1-go1.26.7.darwin-arm64` is published, so `go 1.26.0`
resolves on first build with no manual install.

**Mandatory first action in the viewer task:** run
`go doc github.com/charmbracelet/bubbletea/v2` and
`go doc github.com/charmbracelet/bubbletea/v2.Model`. v2 reshaped `Init`/`View`
signatures and key-message types versus v1. Do not write v1-shaped code from
memory.

## Package layout

```
main.go                     os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
internal/table/             Table + Book interfaces, memTable
internal/layout/            column widths, numeric detection, truncate/pad
internal/source/csvsrc/     delimiter/BOM/encoding sniffing -> Table
internal/source/xlsxsrc/    excelize -> Book/Table
internal/render/            plain-text renderer (print path)
internal/viewer/            Bubble Tea model (interactive path)
internal/cli/               flags, mode decision, wiring, exit codes
```

Both output paths consume the same `table.Table` and the same `layout.Layout`.
The renderer and viewer never parse files; sources never format output.

## Frozen interface contracts

These are contracts between parallel agents. **An agent may not change a
signature another task depends on** — if one looks wrong, stop and report.

### internal/table

```go
package table

type SheetInfo struct {
    Name    string
    Visible bool
}

type Table interface {
    Name() string        // sheet name, or base filename for CSV
    Cols() []string      // header labels
    NRows() int          // data rows, excluding header
    Row(i int) []string  // 0-based; len always == len(Cols())
}

type Book interface {
    Sheets() []SheetInfo
    Table(name string) (Table, error)
}

// New normalises before constructing: width = max(len(cols), longest row).
// cols shorter than width are extended with SyntheticCols names; rows shorter
// than width are padded with "". Returned Table is immutable.
func New(name string, cols []string, rows [][]string) Table

// SyntheticCols returns Excel-style labels: A..Z, AA, AB, ...
func SyntheticCols(n int) []string

// SingleSheetBook wraps one Table as a Book with one visible sheet.
func SingleSheetBook(t Table) Book
```

### internal/layout

```go
package layout

type Col struct {
    Name    string
    Width   int  // display cells
    Numeric bool // right-align
}

type Layout struct{ Cols []Col }

type Options struct {
    MaxColWidth int // default 40 when zero
    MinColWidth int // shrink floor, default 6 when zero
    SampleRows  int // default 1000 when zero
    TotalWidth  int // 0 = unconstrained; >0 = cells available for column CONTENT
}

func Compute(t table.Table, opts Options) Layout

func Truncate(s string, w int) string      // appends "…" when it trims
func Pad(s string, w int, right bool) string // exactly w display cells
```

`TotalWidth` excludes separators — the renderer subtracts its own separator
cost before calling. This keeps layout renderer-agnostic.

**Natural width:** per column, max `runewidth.StringWidth` over the header plus
the first `SampleRows` rows, clamped to `[1, MaxColWidth]`.

**Fit algorithm (must be deterministic):** while `sum(widths) > TotalWidth`,
reduce the widest column by 1 (lowest index wins ties); stop when every column
is at `MinColWidth`. Overflow beyond that is the renderer's problem, not
layout's.

**Numeric detection:** over sampled non-empty cells, strip spaces, `,`, `%` and
a leading `$`/`€`/`£`, then `strconv.ParseFloat`. Numeric when ≥80% parse and at
least one cell was non-empty.

### internal/source/csvsrc

```go
package csvsrc

type Options struct {
    Delim    rune   // 0 = sniff
    NoHeader bool
    Encoding string // "", "utf8", "latin1", "utf16"
}

func Load(r io.Reader, name string, opts Options) (table.Table, error)
func LoadFile(path string, opts Options) (table.Table, error)
func SniffDelim(sample []byte) rune // exported for tests
```

Behaviour:
1. Buffer the first 64 KB as the sniff sample.
2. BOM: strip UTF-8 BOM; on a UTF-16 BOM decode via `x/text/encoding/unicode`.
   `Encoding: "latin1"` uses `charmap.ISO8859_1.NewDecoder()`.
3. Delimiter: for each of `,` `;` `\t` `|`, parse the sample with
   `encoding/csv` (`LazyQuotes: true`, `FieldsPerRecord: -1`); score = count of
   records matching the modal field count; tie-break on higher modal count.
   Require modal count > 1 to win, else fall back to `,`. **Score with
   `encoding/csv`, not raw byte counting** — that is what makes quoted fields
   containing the delimiter score correctly.
4. Header = row 1 unless `NoHeader`, in which case `table.SyntheticCols`.
5. Ragged rows are handled by `table.New`, not here.

### internal/source/xlsxsrc

```go
package xlsxsrc

type Workbook struct{ /* holds *excelize.File */ }

func Open(path string) (*Workbook, error) // satisfies table.Book
func (w *Workbook) Sheets() []table.SheetInfo
func (w *Workbook) Table(name string) (table.Table, error) // built on demand
func (w *Workbook) Close() error
```

Behaviour:
- `GetSheetList()` + `GetSheetVisible()` populate `Sheets()`.
- Read rows via `f.Rows(sheet)` + `rows.Columns()`, which returns
  **format-applied values** — serial `46261` must render as the date Excel
  shows, not the number.
- Trim trailing all-empty rows and columns; Excel routinely claims a used range
  far larger than the real data.
- A password-protected file returns a clear error mentioning encryption, never
  a raw excelize error.
- Merged cells: value lands in the top-left, blanks elsewhere. Accepted
  limitation — document it in the README, do not solve it.

### internal/render

```go
package render

type Options struct {
    Width   int  // 0 = unconstrained (pipe); >0 = terminal width
    Header  bool // default true
    MaxRows int  // 0 = all
}

func Table(w io.Writer, t table.Table, opts Options) error
```

Columns joined by two spaces, header line, then a rule of `-` matching the
computed widths. No box borders — the print path must stay pipe-friendly.
Numeric columns right-aligned.

### internal/viewer

```go
package viewer

type Options struct{ MaxColWidth int }

func Run(bk table.Book, active string, opts Options) error

// Exported for pure tests: feed messages to Update, assert state.
type Model struct{ /* ... */ }
func NewModel(bk table.Book, active string, w, h int, opts Options) Model
```

State: book + active table, cursor row/col, viewport top row and left column,
mode (`normal`/`search`/`inspect`/`help`), search query, sheet list.

Horizontal scroll moves by **whole columns**, never characters — simpler math,
and it never leaves a half-rendered column at the right edge.

| Key | Action |
|---|---|
| `↑ ↓ ← →` / `k j h l` | move cursor |
| `PgUp` `PgDn` / `ctrl+b` `ctrl+f` | page rows |
| `g` / `G` | first / last row |
| `Home` / `End` | first / last column |
| `/` then `Enter` | search: case-insensitive substring, all cells |
| `n` / `N` | next / previous match |
| `Enter` | toggle full untruncated current cell in footer |
| `Tab` / `shift+Tab` | next / previous sheet (bar shown only when >1 sheet) |
| `?` | help overlay |
| `q` / `ctrl+c` | quit |

Search scans on demand from the cursor — no prebuilt match index. Flat memory,
and it stays correct against a future streaming backend. Linear scan of 100k
rows is a few ms.

`WindowSizeMsg` must recompute the layout. This is the single most bug-prone
path in the app; test it explicitly.

### internal/cli

```go
package cli

func Run(args []string, stdout, stderr io.Writer) int

// Pure, unit-testable decision function.
type Mode int
const (ModePrint Mode = iota; ModeViewer)
func chooseMode(forcePrint, forceInteractive, isTTY bool, termW, termH, needW, needH int) Mode
```

Flags (stdlib `flag`):

```
--print / -p        force stdout
--interactive / -i  force viewer
--sheet NAME|N      select sheet
--list-sheets       print sheet names, exit 0
--delim CHAR        override sniffed delimiter
--no-header         row 1 is data; synthesise A,B,C… labels
--encoding ENC      utf8 | latin1 | utf16
--max-col-width N   default 40
--version
```

Source selection by extension, falling back to content: `PK\x03\x04` magic means
xlsx regardless of filename.

Default mode: stdout not a TTY → print. TTY and the rendered table fits the
window → print (small files stay in scrollback). TTY and it does not fit →
viewer.

Exit codes: `0` ok, `1` runtime error (missing/corrupt/encrypted), `2` usage
error.

## Task waves for Sonnet agents

Every task is TDD: failing test first, then code. Every task ends with
`go build ./... && go vet ./... && go test ./...` passing. No task may edit files
outside the paths it owns — that is what makes the parallel waves safe.

### Wave 1 — scaffold (sequential, blocks everything)

**T1** owns `go.mod`, `main.go`, `internal/table/`, `.gitignore`
- `git init`, `go mod init vanlabeke.dev/exshell`, `go 1.26.0`
- Implement the `table` contract above
- Copy this spec to `docs/superpowers/specs/2026-08-26-exshell-design.md`
- Tests: `New` padding, over-long rows extending cols, `SyntheticCols`
  boundaries (`A`, `Z`, `AA`, `AZ`, `BA`), `SingleSheetBook`
- Commit: `scaffold: module, table interface, memTable`

### Wave 2 — three agents in parallel (all depend only on T1)

**T2** owns `internal/source/csvsrc/` (+ its `testdata/`)
Fixtures: comma, semicolon, tab, pipe, UTF-8 BOM, latin-1, quoted field
containing the delimiter, quoted field containing a newline, ragged rows,
header-only, empty file, single column.
Non-negotiable test: the semicolon fixture yields >1 column with **no flags**.

**T3** owns `internal/layout/`
Tests: natural widths, `MaxColWidth` clamp, the deterministic shrink order,
`MinColWidth` floor, `Truncate` with CJK and emoji (width ≠ len), `Pad` both
alignments, numeric detection at the 80% boundary and on `1.234,56` / `$1,200` /
`45%`.

**T4** owns `internal/source/xlsxsrc/` (+ its `testdata/`)
Generate fixtures **with excelize itself** in a test helper, then read back:
multi-sheet, a hidden sheet, date-formatted cells, numeric formats, merged
cells, a trailing empty range. Assert dates come back as dates, not serials.

### Wave 3 — one agent (depends on T1–T4)

**T5** owns `internal/render/` and `internal/cli/`
Golden-file tests of print output at widths 40, 80, 200 under
`internal/render/testdata/`. Unit-test `chooseMode` across the TTY matrix.
Milestone: `exshell testdata/simple.csv` and
`exshell testdata/book.xlsx --list-sheets` work end to end. **Do not start the
viewer before this is green.**

### Wave 4 — one agent (depends on T1, T3, T5)

**T6** owns `internal/viewer/` entirely — core scrolling, search, inspect, help,
sheet switching, in that order. One owner because these all touch the same model;
splitting them across agents would only create conflicts.
Tests treat `Update` as a pure function: feed key and `WindowSizeMsg` messages,
assert cursor/viewport/mode. No terminal required. Run with `-race`.

### Wave 5 — one agent

**T7** owns `README.md`, `Makefile`, error-message polish
`Makefile`: `build`, `test`, `lint`, `install`. README documents the flags, the
keys, and the merged-cell limitation. Then run the full manual verification
below and report results.

## Verification

```bash
go build ./... && go vet ./... && go test ./...
go test -race ./internal/viewer/
```

Manual, after T7:

```bash
./exshell testdata/simple.csv | head -5        # composes like a Unix tool
./exshell testdata/semicolon.csv               # N columns, not 1, no flags
./exshell testdata/dates.xlsx | head -3        # dates, not serial numbers
./exshell testdata/book.xlsx --list-sheets
./exshell testdata/book.xlsx --sheet 2
./exshell testdata/big.csv                     # 100k rows: <1s start, G, /pattern, Enter
./exshell testdata/book.xlsx                   # Tab cycles sheets
./exshell testdata/wide.csv                    # ← → columns; resize mid-view
./exshell nosuchfile.csv;              echo $?  # 1
./exshell testdata/encrypted.xlsx;     echo $?  # 1, message mentions encryption
./exshell --sheet 99 testdata/book.xlsx; echo $? # 2
```

The resize check and the no-flags semicolon check are the two most likely to
expose real bugs — layout recomputation on `WindowSizeMsg` and the sniffer's
scoring are the subtlest pieces of this design.
