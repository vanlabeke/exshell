// Package cli implements the exshell command line: flag parsing, source
// selection (CSV vs XLSX, by extension and content sniffing), the
// print-vs-viewer decision, and dispatch into internal/render or
// internal/viewer.
package cli

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"

	"vanlabeke.dev/exshell/internal/layout"
	"vanlabeke.dev/exshell/internal/render"
	"vanlabeke.dev/exshell/internal/source/csvsrc"
	"vanlabeke.dev/exshell/internal/source/xlsxsrc"
	"vanlabeke.dev/exshell/internal/table"
	"vanlabeke.dev/exshell/internal/viewer"
)

// version is printed by --version. exshell has no release process yet, so
// this is a placeholder the packaging task will wire up properly.
const version = "0.1.0"

// usageText is printed verbatim to stdout for --help/-h. It documents every
// flag plus the interactive viewer's keybindings, since a user who can't get
// into the TUI (piped output, or a --print run) has no other way to
// discover them.
const usageText = `exshell — view CSV and XLSX files in a terminal

Usage:
  exshell [flags] <file>

exshell prints small files as an aligned plain-text table and opens larger
ones in an interactive viewer. Piped or redirected output is always
plain-text, so exshell composes with other Unix tools (less, grep, ...).

Flags:
  -p, --print              force plain-text output, even to a terminal
  -i, --interactive        force the interactive viewer, even when piped
      --sheet <name|#>     sheet to open: by name, or 1-based index
      --list-sheets        print sheet names and exit
      --delim <char>       override the sniffed CSV delimiter
      --no-header          treat row 1 as data; synthesize A, B, C... labels
      --encoding <enc>     input encoding: utf8, latin1, or utf16
      --max-col-width <n>  maximum column width (default 40)
      --version            print the version and exit
  -h, --help                show this help and exit

Interactive viewer keybindings (once open):
  up/down/left/right, k/j/h/l    move the cursor
  PgUp/PgDn, ctrl+b/ctrl+f       page up/down
  g / G                          jump to first / last row
  Home / End                     jump to first / last column
  /                              search (Enter runs it, Esc cancels)
  n / N                          jump to next / previous match
  Enter                          toggle inspect mode (full cell value)
  Tab / shift+Tab                switch to next / previous sheet
  ?                              toggle the in-app help screen
  q, ctrl+c                      quit

Examples:
  exshell data.csv                  # print or open the viewer, by size
  exshell data.xlsx | less -S       # always plain text, pipe-friendly
  exshell --list-sheets book.xlsx
  exshell --sheet 2 book.xlsx
  exshell book.xlsx --sheet 2       # flags and the file may be in any order
`

// Mode selects between the plain-text print path and the interactive
// viewer.
type Mode int

const (
	ModePrint Mode = iota
	ModeViewer
)

// zipMagic is the four-byte signature of a zip archive, which is what an
// xlsx file actually is on disk. Content sniffing on this magic lets a
// misnamed file (e.g. a real .xlsx saved with a .csv extension) still be
// routed correctly.
var zipMagic = []byte{'P', 'K', 0x03, 0x04}

// chooseMode decides between the plain-text print path and the interactive
// viewer. It is pure: no globals, no syscalls, so it is exhaustively
// unit-testable across the whole decision matrix. Precedence, in order:
//
//  1. forcePrint always wins (even over forceInteractive).
//  2. forceInteractive wins over everything else.
//  3. A non-TTY destination (piped or redirected) is never a TUI.
//  4. A TTY whose size could not be measured (termW <= 0 || termH <= 0 —
//     term.GetSize failed, or genuinely reported a 0x0 window, both of
//     which happen in the wild: `script`, some pty-allocating CI runners,
//     `ssh -t` and container setups, editor-embedded terminals mid-startup)
//     is a reason to print, not to seize the screen: seizing it on a size
//     we can't trust is what used to hang the process. Print degrades
//     gracefully; the viewer does not.
//  5. A TTY that fits stays in scrollback: needW <= termW && needH <=
//     termH-1 (the -1 leaves a line for the shell prompt).
//  6. Otherwise, the interactive viewer.
func chooseMode(forcePrint, forceInteractive, isTTY bool, termW, termH, needW, needH int) Mode {
	switch {
	case forcePrint:
		return ModePrint
	case forceInteractive:
		return ModeViewer
	case !isTTY:
		return ModePrint
	case termW <= 0 || termH <= 0:
		return ModePrint
	case needW <= termW && needH <= termH-1:
		return ModePrint
	default:
		return ModeViewer
	}
}

// Run executes the exshell CLI for args (as os.Args[1:] would be), writing
// only to stdout/stderr — never to os.Stdout/os.Stderr directly, so callers
// (including tests) can capture output. It returns the process exit code:
// 0 ok, 1 runtime error, 2 usage error.
func Run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("exshell", flag.ContinueOnError)
	// Suppress the flag package's own usage dump on error: every error
	// exshell reports goes through stderr with an "exshell: " prefix
	// instead, including flag-parsing errors.
	fs.SetOutput(io.Discard)

	var (
		forcePrint, forceInteractive bool
		listSheets, noHeader         bool
		showVersion                  bool
		sheet, delim, encoding       string
		maxColWidth                  int
	)

	fs.BoolVar(&forcePrint, "print", false, "force plain-text output even to a terminal")
	fs.BoolVar(&forcePrint, "p", false, "force plain-text output even to a terminal")
	fs.BoolVar(&forceInteractive, "interactive", false, "force the interactive viewer even when piped")
	fs.BoolVar(&forceInteractive, "i", false, "force the interactive viewer even when piped")
	fs.StringVar(&sheet, "sheet", "", "sheet by name, or 1-based index")
	fs.BoolVar(&listSheets, "list-sheets", false, "print sheet names and exit")
	fs.StringVar(&delim, "delim", "", "override the sniffed delimiter")
	fs.BoolVar(&noHeader, "no-header", false, "treat row 1 as data; synthesize A,B,C... labels")
	fs.StringVar(&encoding, "encoding", "", "utf8 | latin1 | utf16")
	fs.IntVar(&maxColWidth, "max-col-width", 0, "maximum column width (default 40)")
	fs.BoolVar(&showVersion, "version", false, "print version and exit")

	// flag.Parse alone stops at the first non-flag token and treats
	// everything after it as positional, so "exshell data.csv --sheet 2"
	// (file first, flags after — how most people type a command) would
	// otherwise misparse as two file arguments instead of one file plus a
	// flag. Parse repeatedly, peeling off one positional per pass, so
	// flags and the file argument may appear in any order; this delegates
	// entirely to flag's own value-vs-flag-vs-positional logic, so e.g.
	// "--delim ';' f.csv" still consumes ';' as --delim's value rather
	// than mistaking it for the file.
	var positionals []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			if err == flag.ErrHelp {
				fmt.Fprint(stdout, usageText)
				return 0
			}
			fmt.Fprintf(stderr, "exshell: %v\n", err)
			return 2
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		positionals = append(positionals, rest[0])
		rest = rest[1:]
	}

	if showVersion {
		fmt.Fprintf(stdout, "exshell %s\n", version)
		return 0
	}

	switch encoding {
	case "", "utf8", "latin1", "utf16":
	default:
		fmt.Fprintf(stderr, "exshell: invalid --encoding value %q (want utf8, latin1, or utf16)\n", encoding)
		return 2
	}

	// maxColWidth's zero value is ambiguous by construction (IntVar's
	// default and a literal "--max-col-width 0" both parse to 0), so
	// fs.Visit — not the value alone — is what tells "not supplied" apart
	// from "supplied 0". That distinction matters twice: here, only an
	// explicitly supplied value is validated (0 unsupplied is not a user
	// error, it means "let layout apply its own default"); and downstream,
	// render.Table treats an unsupplied maxColWidth (still 0 past this
	// point) as license to drop the default cap on unconstrained output —
	// see I1/R18 in render.go. A supplied value below 1 is rejected here as
	// a usage error rather than reaching layout.Compute at all: layout
	// guards its own Width>=1 invariant defensively regardless, but a
	// negative or zero cap is never something the user meant, so it should
	// never render a blank grid or come within a mile of render.go's
	// strings.Repeat panic.
	var maxColWidthSet bool
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "max-col-width" {
			maxColWidthSet = true
		}
	})
	if maxColWidthSet && maxColWidth < 1 {
		fmt.Fprintf(stderr, "exshell: --max-col-width must be at least 1, got %d\n", maxColWidth)
		return 2
	}

	switch len(positionals) {
	case 0:
		fmt.Fprintln(stderr, "exshell: no file argument")
		return 2
	case 1:
	default:
		fmt.Fprintf(stderr, "exshell: exactly one file argument required, got %d\n", len(positionals))
		return 2
	}
	path := positionals[0]

	var delimRune rune
	if delim != "" {
		runes := []rune(delim)
		if len(runes) != 1 {
			fmt.Fprintf(stderr, "exshell: --delim must be a single character, got %q\n", delim)
			return 2
		}
		delimRune = runes[0]
		// encoding/csv rejects these four runes as a Comma value (they
		// collide with quoting, line-ending, or invalid-rune handling
		// inside the reader itself). Reject them here instead of letting
		// csv.Reader do it: that failure would otherwise surface as a
		// runtime error carrying a bare "csv: ..." prefix — a usage
		// mistake wearing a runtime-error exit code.
		switch delimRune {
		case '"', '\r', '\n', utf8.RuneError:
			fmt.Fprintf(stderr, "exshell: --delim %q is not a valid CSV delimiter\n", delim)
			return 2
		}
	}

	bk, closeBook, err := openBook(path, csvsrc.Options{
		Delim:    delimRune,
		NoHeader: noHeader,
		Encoding: encoding,
	})
	if err != nil {
		fmt.Fprintf(stderr, "exshell: %v\n", err)
		return 1
	}
	defer closeBook()

	sheets := bk.Sheets()

	if listSheets {
		for _, s := range sheets {
			fmt.Fprintln(stdout, s.Name)
		}
		return 0
	}

	if len(sheets) == 0 {
		fmt.Fprintf(stderr, "exshell: %s: workbook has no sheets\n", path)
		return 1
	}

	targetName, err := resolveSheet(sheets, sheet)
	if err != nil {
		fmt.Fprintf(stderr, "exshell: %v\n", err)
		return 2
	}

	tbl, err := bk.Table(targetName)
	if err != nil {
		fmt.Fprintf(stderr, "exshell: %v\n", err)
		return 1
	}

	// A table with zero columns has nothing to render, so an empty grid at
	// exit 0 is worse than failing loudly (R6) — indistinguishable from
	// success. csvsrc already enforces this at load time for its own
	// zero-byte-file case with a more specific "file is empty" message, so
	// it never reaches here with zero columns; this check exists for the
	// case that does: an empty xlsx sheet, which xlsxsrc legitimately
	// returns as a valid, columnless table.New(name, nil, nil) rather than
	// an error, since "empty sheet" isn't itself invalid the way an
	// unreadable file is.
	if len(tbl.Cols()) == 0 {
		fmt.Fprintf(stderr, "exshell: %s: sheet %q has no columns\n", path, targetName)
		return 1
	}

	isTTY, termW, termH := termInfo(stdout)
	needW, needH := renderNeeds(tbl, maxColWidth)

	mode := chooseMode(forcePrint, forceInteractive, isTTY, termW, termH, needW, needH)

	switch mode {
	case ModeViewer:
		if err := viewer.Run(bk, targetName, stdout, viewer.Options{MaxColWidth: maxColWidth}); err != nil {
			fmt.Fprintf(stderr, "exshell: %v\n", err)
			return 1
		}
		return 0
	default:
		w := 0
		if isTTY {
			w = termW
		}
		opts := render.Options{Width: w, Header: true, MaxColWidth: maxColWidth}
		if err := render.Table(stdout, tbl, opts); err != nil {
			fmt.Fprintf(stderr, "exshell: %v\n", err)
			return 1
		}
		return 0
	}
}

// renderNeeds computes the width and height a table needs to display in
// full, unconstrained: needW is sum(column widths) + separator cost from
// an unconstrained layout.Compute; needH is NRows()+2 (a header row and a
// rule line are always shown by the print path).
func renderNeeds(t table.Table, maxColWidth int) (needW, needH int) {
	lay := layout.Compute(t, layout.Options{MaxColWidth: maxColWidth})
	n := len(lay.Cols)
	sum := 0
	for _, c := range lay.Cols {
		sum += c.Width
	}
	return sum + layout.SepCost(n), t.NRows() + 2
}

// termInfo reports whether stdout is a terminal, and its size if so. It is
// the one place that touches syscalls/global terminal state; the mapping
// from a raw term.GetSize result to (isTTY, width, height) is factored out
// into termSizeResult below, kept pure and separate, so it — and therefore
// the case chooseMode needs (a real but unmeasurable terminal) — is
// directly unit-testable without a syscall in sight.
func termInfo(stdout io.Writer) (isTTY bool, width, height int) {
	f, ok := stdout.(*os.File)
	if !ok {
		return false, 0, 0
	}
	fd := int(f.Fd())
	if !term.IsTerminal(fd) {
		return false, 0, 0
	}
	w, h, err := term.GetSize(fd)
	return termSizeResult(err, w, h)
}

// termSizeResult maps the raw result of term.GetSize on a file already
// confirmed to be a terminal into (isTTY, width, height); isTTY is always
// true here. A GetSize error is treated identically to a genuine 0x0
// result — both leave the terminal's size unmeasurable, which is exactly
// the case chooseMode's termW<=0||termH<=0 branch exists to catch, so both
// must map to the same (0, 0) rather than an error leaking through with
// stale or undefined w/h values.
func termSizeResult(getSizeErr error, w, h int) (isTTY bool, width, height int) {
	if getSizeErr != nil {
		return true, 0, 0
	}
	return true, w, h
}

// openBook opens path as a table.Book: xlsxsrc for an .xlsx/.xlsm
// extension or zip-magic content, csvsrc.LoadFile otherwise. The returned
// close func releases any underlying file handle (a no-op for CSV, whose
// data is fully read into memory by csvsrc.Load) and is always non-nil.
func openBook(path string, csvOpts csvsrc.Options) (table.Book, func() error, error) {
	if isXLSXPath(path) {
		wb, err := xlsxsrc.Open(path)
		if err != nil {
			return nil, nil, err
		}
		return wb, wb.Close, nil
	}

	tbl, err := csvsrc.LoadFile(path, csvOpts)
	if err != nil {
		return nil, nil, err
	}
	return table.SingleSheetBook(tbl), func() error { return nil }, nil
}

// isXLSXPath decides whether path should be read as xlsx: a recognized
// xlsx/xlsm extension always wins (so an OLE2 legacy/encrypted xlsx still
// reaches xlsxsrc.Open, which reports that ambiguity clearly); otherwise
// the file's first four bytes are sniffed for the zip magic number, so a
// misnamed xlsx (e.g. saved with a .csv extension) is still routed
// correctly regardless of its name. A file that can't be opened for
// sniffing is treated as not-xlsx; the real error surfaces later when the
// chosen reader tries to open it for real.
func isXLSXPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xlsx", ".xlsm":
		return true
	}
	return hasZipMagic(path)
}

func hasZipMagic(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	header := make([]byte, len(zipMagic))
	n, _ := io.ReadFull(f, header)
	if n < len(zipMagic) {
		return false
	}
	return bytes.Equal(header, zipMagic)
}

// resolveSheet picks the sheet to display out of sheets (always non-empty)
// per the --sheet flag value want:
//
//   - "" (not given): the first visible sheet, or sheets[0] if none are
//     visible.
//   - a value that parses as an integer: a 1-based index into sheets, in
//     workbook order, visible or not.
//   - anything else: an exact, case-sensitive sheet name match.
func resolveSheet(sheets []table.SheetInfo, want string) (string, error) {
	if want == "" {
		for _, s := range sheets {
			if s.Visible {
				return s.Name, nil
			}
		}
		return sheets[0].Name, nil
	}

	if idx, err := strconv.Atoi(want); err == nil {
		if idx < 1 || idx > len(sheets) {
			return "", fmt.Errorf("sheet index %d out of range (1-%d)", idx, len(sheets))
		}
		return sheets[idx-1].Name, nil
	}

	for _, s := range sheets {
		if s.Name == want {
			return s.Name, nil
		}
	}
	return "", fmt.Errorf("no sheet named %q", want)
}
