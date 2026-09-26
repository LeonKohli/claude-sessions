package cli

import (
	"fmt"
	"io"
)

// printer renders the human-readable form of a result. Text output is plain
// stdout for a terminal or a pipe, never markup, so no escaping applies.
type printer struct{ w io.Writer }

// line writes one formatted line, appending the newline.
func (p printer) line(format string, args ...any) {
	_, _ = io.WriteString(p.w, fmt.Sprintf(format, args...)+"\n")
}

// blank writes an empty line.
func (p printer) blank() {
	_, _ = io.WriteString(p.w, "\n")
}

// raw writes text exactly as given.
func (p printer) raw(s string) {
	_, _ = io.WriteString(p.w, s)
}
