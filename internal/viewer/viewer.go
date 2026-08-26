// Package viewer will hold the interactive TUI for exshell. It is a later
// task; this file exists only so that internal/cli can compile and the
// plain-text print path can ship standalone. The next task owns this
// package entirely and will replace this file (controller ruling R3).
package viewer

import (
	"errors"

	"vanlabeke.dev/exshell/internal/table"
)

// Options controls the interactive viewer. Fields will grow when the real
// viewer is implemented.
type Options struct {
	MaxColWidth int
}

// Run is not implemented yet. It always returns a non-nil error so callers
// (internal/cli) surface a clear message and exit non-zero instead of
// silently doing nothing.
func Run(bk table.Book, active string, opts Options) error {
	return errors.New("viewer: interactive mode is not implemented yet")
}
