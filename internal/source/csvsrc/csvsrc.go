// Package csvsrc reads CSV files into a table.Table, sniffing delimiter,
// byte-order mark, and character encoding so that no flags are required for
// well-formed real-world CSV — including a semicolon-delimited export from a
// European Excel install.
package csvsrc

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"

	"vanlabeke.dev/exshell/internal/table"
)

// sampleSize is the number of bytes buffered for dialect sniffing.
const sampleSize = 64 * 1024

// Options controls how Load interprets a CSV stream. The zero value means
// "sniff everything": delimiter, header, and encoding.
type Options struct {
	Delim    rune   // 0 = sniff
	NoHeader bool   // treat row 1 as data and synthesize column labels
	Encoding string // "", "utf8", "latin1", "utf16"
}

// LoadFile opens path and loads it as CSV. Name() on the returned table is
// the base filename of path.
func LoadFile(path string, opts Options) (table.Table, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Load(f, filepath.Base(path), opts)
}

// Load reads r as CSV, sniffing delimiter and encoding unless overridden by
// opts, and returns a normalised table.Table named name.
//
// Zero bytes of input, or input that yields zero columns, is an error. A
// header-only file (a header row and no data rows) is valid.
func Load(r io.Reader, name string, opts Options) (table.Table, error) {
	// Buffer sampleSize+1 bytes. Reading one byte past sampleSize tells us
	// whether the sample was truncated mid-stream, without losing that byte
	// from the reconstructed full stream (it goes right back into the
	// io.MultiReader below).
	buf := make([]byte, sampleSize+1)
	n, err := io.ReadFull(r, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("csvsrc: %s: %w", name, err)
	}
	read := buf[:n]

	if len(read) == 0 {
		return nil, fmt.Errorf("csvsrc: %s: file is empty", name)
	}

	truncated := n > sampleSize
	sample := read
	if truncated {
		sample = read[:sampleSize]
	}

	// Reconstruct the full stream: the bytes already read, followed by
	// whatever remains of r.
	full := io.MultiReader(bytes.NewReader(read), r)
	decodedFull := transform.NewReader(full, newDecoder(opts))

	delim := opts.Delim
	if delim == 0 {
		sniffSample := sample
		if truncated {
			// The 64 KB cut may have landed mid-record. Drop the dangling
			// tail so it can't skew delimiter scoring.
			sniffSample = trimToLastNewline(sniffSample)
		}
		decodedSniff, _, _ := transform.Bytes(newDecoder(opts), sniffSample)
		delim = SniffDelim(decodedSniff)
	}

	cr := csv.NewReader(decodedFull)
	cr.Comma = delim
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1

	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csvsrc: %s: %w", name, err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("csvsrc: %s: file is empty", name)
	}

	var cols []string
	var rows [][]string
	if opts.NoHeader {
		width := 0
		for _, rec := range records {
			if len(rec) > width {
				width = len(rec)
			}
		}
		cols = table.SyntheticCols(width)
		rows = records
	} else {
		cols = records[0]
		rows = records[1:]
	}

	if len(cols) == 0 {
		return nil, fmt.Errorf("csvsrc: %s: file is empty", name)
	}

	return table.New(name, cols, rows), nil
}

// SniffDelim scores each candidate delimiter (',', ';', '\t', '|') by
// parsing sample with encoding/csv (LazyQuotes, FieldsPerRecord: -1) and
// counting how many records have that candidate's modal field count. The
// highest score wins; ties break on the higher modal field count. A
// candidate must reach a modal field count greater than 1 to win at all; if
// none does, ',' is returned.
//
// Scoring is always done with encoding/csv, never by counting raw delimiter
// bytes — that is what makes a quoted field containing the delimiter score
// correctly.
func SniffDelim(sample []byte) rune {
	candidates := []rune{',', ';', '\t', '|'}

	type stat struct{ modal, score int }
	stats := make(map[rune]stat, len(candidates))

	for _, d := range candidates {
		cr := csv.NewReader(bytes.NewReader(sample))
		cr.Comma = d
		cr.LazyQuotes = true
		cr.FieldsPerRecord = -1

		records, err := cr.ReadAll()
		if err != nil {
			// A candidate that can't even parse the sample can't win.
			continue
		}

		counts := make(map[int]int)
		for _, rec := range records {
			counts[len(rec)]++
		}

		var modal, score int
		for fieldCount, cnt := range counts {
			if cnt > score || (cnt == score && fieldCount > modal) {
				modal, score = fieldCount, cnt
			}
		}
		stats[d] = stat{modal: modal, score: score}
	}

	best := ','
	bestScore, bestModal := -1, -1
	for _, d := range candidates {
		s := stats[d]
		if s.modal <= 1 {
			// Never actually split anything; not a real candidate.
			continue
		}
		if s.score > bestScore || (s.score == bestScore && s.modal > bestModal) {
			best, bestScore, bestModal = d, s.score, s.modal
		}
	}
	return best
}

// newDecoder returns a fresh transform.Transformer decoding a raw CSV byte
// stream to UTF-8 per opts. An explicit opts.Encoding always wins. With no
// explicit encoding, a leading UTF-8 or UTF-16 (LE/BE) byte-order mark is
// detected and stripped/decoded; unmarked input passes through unchanged.
func newDecoder(opts Options) transform.Transformer {
	switch opts.Encoding {
	case "latin1":
		return charmap.ISO8859_1.NewDecoder()
	case "utf16":
		// Forced UTF-16: still honor a BOM if present, but assume
		// little-endian with no BOM rather than refusing to decode.
		return unicode.BOMOverride(unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder())
	default:
		// "" and "utf8": sniff a BOM (UTF-8 or UTF-16); otherwise pass
		// bytes through unchanged.
		return unicode.BOMOverride(encoding.Nop.NewDecoder())
	}
}

// trimToLastNewline drops any content after the final '\n' in b. It is used
// to discard a final record left dangling by the 64 KB sniff-sample cut, so
// that record does not skew delimiter scoring.
func trimToLastNewline(b []byte) []byte {
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		return b[:i+1]
	}
	return b
}
