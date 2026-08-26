# exshell

`exshell` ("excel" + "shell") is a terminal viewer for CSV and XLSX files. It
is a **viewer, not a spreadsheet**: no editing, no formulas, no sorting, no
filtering — just a fast, correctly-aligned, correctly-typed look at tabular
data without leaving the terminal.

## Why

Reading tabular data in a terminal is awkward today. `cat` on a CSV gives
unaligned comma soup, `column -t` breaks on quoted fields that contain the
delimiter, and an `.xlsx` file can't be read at all without opening Excel or
LibreOffice. `exshell somefile.xlsx` shows readable, aligned data in under a
second, and `exshell somefile.csv | grep foo` behaves like any other Unix
tool.

## Install / build

Requires Go 1.26 or later.

```bash
git clone <this repo>
cd exshell
make build          # -> ./exshell, run it from here
make install        # -> /usr/local/bin/exshell, on your $PATH
make uninstall      # removes it again
```

`make install` builds for your own platform and copies the binary into
`/usr/local/bin`. That directory is root-owned, so it will prompt for your
password — but note that only the *copy* is elevated, not the build.

**Run it as plain `make install`, not `sudo make install`.** Under `sudo` the
Go build itself runs as root and leaves root-owned files in your module cache
(`~/go/pkg/mod`), which then breaks ordinary builds as yourself.

To install somewhere you already own — no password, no `sudo` invoked at all:

```bash
make install PREFIX=$HOME/.local     # -> ~/.local/bin/exshell
make install BINDIR=/opt/bin         # or set the bin directory outright
```

Pass the same `PREFIX`/`BINDIR` to `make uninstall` that you passed to
`make install`.

There's no release process yet, and no cross-compiled binaries — building
from source is the only path today.

## Usage

`exshell` decides how to show a file based on where its output is going:

- **Piped or redirected** (`exshell f.csv | less`, `exshell f.csv > out.txt`):
  always plain, aligned text. This is what makes `exshell` compose with the
  rest of a Unix pipeline.
- **A terminal, and the data fits** the current window: also plain text,
  printed straight to the scrollback — no need for a full-screen viewer to
  read ten rows.
- **A terminal, and the data doesn't fit**: the interactive viewer opens
  automatically.

`--print`/`-p` and `--interactive`/`-i` override that decision either way.

### Examples

```bash
# Small file: prints straight to the terminal.
exshell testdata/simple.csv

# Force plain text even on a terminal, and pipe it onward like any tool.
exshell testdata/book.xlsx --print | less -S
exshell testdata/dates.xlsx | head -3

# List sheet names, then open a specific one. Flags and the file argument
# may appear in either order.
exshell --list-sheets testdata/book.xlsx
exshell testdata/book.xlsx --sheet 2
exshell --sheet Employees testdata/book.xlsx

# A semicolon-delimited European Excel export — no flags needed, the
# delimiter is sniffed automatically.
exshell testdata/semicolon.csv

# A big file: opens the interactive viewer (or force it explicitly).
exshell testdata/big.csv
exshell --interactive testdata/simple.csv
```

### Flags

| Flag | Description |
|---|---|
| `-p`, `--print` | force plain-text output, even to a terminal |
| `-i`, `--interactive` | force the interactive viewer, even when piped |
| `--sheet <name\|#>` | sheet to open: by name, or 1-based index |
| `--list-sheets` | print sheet names and exit |
| `--delim <char>` | override the sniffed CSV delimiter |
| `--no-header` | treat row 1 as data; synthesize `A`, `B`, `C`... column labels |
| `--encoding <enc>` | input encoding: `utf8`, `latin1`, or `utf16` |
| `--max-col-width <n>` | maximum column width (default 40) |
| `--version` | print the version and exit |
| `-h`, `--help` | print usage and the interactive keybindings, then exit 0 |

Exit codes: `0` success, `1` a runtime error (bad/missing/unreadable file,
etc.), `2` a usage error (bad flags, out-of-range `--sheet`).

### Interactive viewer keybindings

| Key(s) | Action |
|---|---|
| `up`/`down`/`left`/`right`, `h`/`j`/`k`/`l` | move the cursor |
| `PgUp`/`PgDn`, `ctrl+b`/`ctrl+f` | page up/down |
| `g` / `G` | jump to first / last row |
| `Home` / `End` | jump to first / last column |
| `/` | open search (`Enter` runs it, `Esc` cancels) |
| `n` / `N` | jump to next / previous search match |
| `Enter` | toggle inspect mode (shows the full, untruncated value of the current cell) |
| `Tab` / `shift+Tab` | switch to the next / previous sheet |
| `?` | toggle the in-app help screen |
| `q`, `ctrl+c` | quit |

A workbook's hidden sheets are still reachable via `Tab`/`shift+Tab` — they
are marked `(hidden)` in the tab bar rather than being skipped, since
`exshell` is an inspection tool and `--sheet` can already reach them.

## Limitations (known, accepted, not bugs)

- **Merged xlsx cells only show a value in their top-left cell.** When cells
  are merged (e.g. a section header spanning several columns), the underlying
  `excelize` library stores the value in the top-left cell of the merge only;
  every other cell the merge covers reads back as an empty string, not the
  merged value repeated. `exshell` shows exactly that — it does not detect
  merges or broadcast the value across the merged range. A merged header row
  will look like its text appears in only the first of the columns it
  visually spans.
- **`exshell` is a viewer, not an editor.** No editing, no formulas, no
  sorting, no filtering. It shows you what's in the file; changing the file
  is someone else's job.
- **The whole file is held in memory.** This is fine for the everyday
  exports `exshell` targets — CSV/XLSX files people actually email around —
  but a multi-gigabyte file is out of scope. The core `table.Table`
  interface exists precisely so an indexed, on-disk backend could replace
  the current in-memory one later without changing any consumer (the
  renderer, the viewer, or the CLI).
- **Row 1 of every sheet/CSV is always the header**, unconditionally (or
  use `--no-header` to synthesize `A, B, C...` labels instead) — there's no
  header-detection heuristic.
- **Only trailing empty rows/columns are trimmed.** An xlsx sheet that
  claims a much larger "used range" than it actually has data in (routine in
  real files) gets that dead trailing tail dropped. Interior empty
  rows/columns, inside the real data, are preserved verbatim — `exshell`
  never compacts or reflows a sheet's blank rows/columns.
- **Password-protected `.xlsx` files fail closed.** `exshell` never prompts
  for a password; opening an encrypted workbook returns a clear error saying
  so (exit code 1) rather than attempting to guess or bypass it. The same
  error also covers the (much rarer) case of a legacy pre-2007
  `.xls`/`.doc`/`.ppt` file renamed to `.xlsx` — both share the same
  underlying container format, and the message names both possibilities
  rather than asserting encryption as fact.
- **The viewer's column widths are fixed per sheet**, computed once when the
  sheet loads and independent of terminal width; resizing the terminal
  recomputes how many rows/columns are *visible*, not how wide each column
  is. Horizontal scrolling moves a whole column at a time, not character by
  character.
- **No mouse support.** The viewer is keyboard-only; clicking or scrolling
  with a mouse does nothing.
- **`--sheet`/tab-bar reach hidden sheets deliberately.** A sheet marked
  hidden in the workbook still shows up (marked `(hidden)`) — `exshell` is
  an inspection tool, and hiding it from navigation while `--sheet` can
  still reach it directly would be inconsistent.

## Fixtures / development

`testdata/*.csv` and `testdata/*.xlsx` (except `big.csv`, which is
git-ignored for size) are committed, generated fixtures — see
`cmd/genfixtures/main.go` for exactly how each one is built, and regenerate
them with:

```bash
make fixtures
```

Other Makefile targets: `make build`, `make test`, `make lint` (`go vet` +
a `gofmt -l` check — no external linter is assumed installed), `make
install`.

## Project layout

```
main.go                    entry point
internal/cli/               flag parsing, print-vs-viewer decision, dispatch
internal/table/              the Table/Book interfaces + in-memory implementation
internal/source/csvsrc/      CSV reading: dialect/BOM/encoding sniffing
internal/source/xlsxsrc/     XLSX reading, via excelize
internal/layout/             column widths, numeric detection, truncation
internal/render/             the plain-text print path
internal/viewer/             the interactive Bubble Tea v2 TUI
cmd/genfixtures/             generates testdata/*
```
