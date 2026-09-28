// Package cli is the non-interactive surface of agent-sessions, built for
// consumption by coding agents rather than humans at a terminal.
//
// It follows the CLI Spec (https://clispec.dev) v0.2: structured output with
// TTY auto-detection, schema introspection, data on stdout and everything else
// on stderr, no interactive prompts, and bounded output by default.
package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// SchemaVersion is the envelope contract. Consumers should check it before
// decoding data, which may evolve independently.
const SchemaVersion = 1

// Format selects how a command renders its result.
type Format string

const (
	FormatAuto Format = "auto" // json when piped, text on a terminal
	FormatJSON Format = "json"
	FormatText Format = "text"
)

// Envelope wraps every JSON response.
type Envelope struct {
	OK     bool   `json:"ok"`
	Schema int    `json:"schema"`
	Cmd    string `json:"cmd"`
	Data   any    `json:"data,omitempty"`
	Error  *Fault `json:"error,omitempty"`
	// Truncated reports that bounded output dropped results, so a consumer
	// knows the answer is partial rather than complete.
	Truncated *Truncation `json:"truncated,omitempty"`
}

// Fault is a machine-readable failure.
type Fault struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func (f *Fault) Error() string { return f.Message }

// Truncation describes output the limits cut off.
//
// Total is omitted when the command did not count the full set. Commands that
// read lazily must not invent a total: reporting the size of the window they
// happened to read would tell a consumer it had everything after one more
// call, which is worse than admitting the count is unknown.
type Truncation struct {
	Returned int    `json:"returned"`
	Total    int    `json:"total,omitempty"`
	HasMore  bool   `json:"has_more"`
	Hint     string `json:"hint"`
}

// Stable error codes.
const (
	CodeUsage         = "usage_error"
	CodeNotFound      = "not_found"
	CodeAmbiguous     = "ambiguous_id"
	CodeUnavailable   = "store_unavailable"
	CodeUnrecoverable = "not_recoverable"
	CodeInternal      = "internal_error"
)

// IsTTY reports whether w is an interactive terminal.
func IsTTY(w *os.File) bool {
	return term.IsTerminal(int(w.Fd()))
}

// resolve turns auto into a concrete format using TTY detection, so a piped
// invocation is machine-readable without the caller passing a flag.
func (f Format) resolve(out *os.File) Format {
	if f != FormatAuto {
		return f
	}
	if IsTTY(out) {
		return FormatText
	}
	return FormatJSON
}

// ParseFormat validates an --output value.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatAuto, FormatJSON, FormatText:
		return Format(s), nil
	default:
		return "", fmt.Errorf("invalid --output %q: want auto, json, or text", s)
	}
}

// Result is what a command produces: a payload plus its human rendering.
type Result struct {
	Data      any
	Truncated *Truncation
	// Text renders the human form. Commands that emit raw bytes (file
	// content) set Raw instead, which bypasses both envelope and formatting.
	Text func(io.Writer)
	Raw  []byte
}

// Emit writes a successful result in the requested format.
func Emit(out *os.File, format Format, cmd string, r Result) error {
	if r.Raw != nil {
		_, err := out.Write(r.Raw)
		return err
	}

	switch format.resolve(out) {
	case FormatJSON:
		enc := json.NewEncoder(out)
		enc.SetEscapeHTML(false)
		return enc.Encode(Envelope{
			OK: true, Schema: SchemaVersion, Cmd: cmd,
			Data: r.Data, Truncated: r.Truncated,
		})
	default:
		buffer := bufio.NewWriter(out)
		if r.Text != nil {
			r.Text(buffer)
		}
		// Truncation is a correctness signal, not decoration: a human reading
		// text output must also know the list was cut short.
		if t := r.Truncated; t != nil {
			if t.Total > 0 {
				fmt.Fprintf(buffer, "\n(%d of %d shown — %s)\n", t.Returned, t.Total, t.Hint)
			} else {
				fmt.Fprintf(buffer, "\n(%d shown — %s)\n", t.Returned, t.Hint)
			}
		}
		return buffer.Flush()
	}
}

// Fail reports an error. Diagnostics go to stderr so a consumer piping stdout
// never has to disentangle them from data.
func Fail(format Format, cmd, code, message, hint string) int {
	fault := &Fault{Code: code, Message: message, Hint: hint}

	if format.resolve(os.Stdout) == FormatJSON {
		enc := json.NewEncoder(os.Stderr)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(Envelope{OK: false, Schema: SchemaVersion, Cmd: cmd, Error: fault})
	} else {
		fmt.Fprintf(os.Stderr, "error: %s\n", message)
		if hint != "" {
			fmt.Fprintf(os.Stderr, "hint: %s\n", hint)
		}
	}
	return 1
}
