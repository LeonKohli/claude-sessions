package cli

import "strings"

// boolFlags take no value, so a following token is a positional argument
// rather than the flag's operand.
var boolFlags = map[string]bool{
	"subagents": true,
	"claude":    true,
	"codex":     true,
	"help":      true,
	"h":         true,
}

// flagAliases maps spellings a model is likely to produce onto the canonical
// flag. Agents generalise from other tools in this space — `--json` is the
// house style of cass, ctx and the codex session managers — and from the
// snake_case of Python APIs. Rejecting those costs a whole turn to recover
// from, so accepting them is cheaper than being right about spelling.
var flagAliases = map[string]string{
	"json":        "output=json",
	"robot":       "output=json",
	"format":      "output",
	"max_results": "limit",
	"max-results": "limit",
	"num":         "limit",
	"n":           "limit",
	"max_chars":   "max-chars",
	"maxchars":    "max-chars",
	"provider":    "agent",
	"tool":        "agent",
	"cwd":         "project",
	"workspace":   "project",
}

// positionalAliases name an operand that belongs in positional place.
var positionalAliases = map[string]bool{
	"query": true, "q": true, "text": true, "pattern": true,
	"id": true, "session": true, "path": true, "file": true,
}

// browserFlags are the only flags the interactive browser accepts. Every other
// flag implies the caller wants the non-interactive surface.
var browserFlags = map[string]bool{"claude": true, "codex": true}

// normalizeArgs rewrites alias spellings and lifts named operands into
// positional place, before the flag package ever sees them.
func normalizeArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" || a == "--" {
			out = append(out, a)
			continue
		}

		name := strings.TrimLeft(a, "-")
		value, hasValue := "", false
		if j := strings.IndexByte(name, '='); j >= 0 {
			name, value, hasValue = name[:j], name[j+1:], true
		}

		// --query "auth" is the same request as a bare operand.
		if positionalAliases[name] {
			if hasValue {
				out = append(out, value)
			} else if i+1 < len(args) {
				i++
				out = append(out, args[i])
			}
			continue
		}

		canonical, ok := flagAliases[name]
		if !ok {
			out = append(out, a)
			continue
		}
		// An alias may carry its own value, as `--json` does.
		if k := strings.IndexByte(canonical, '='); k >= 0 {
			out = append(out, "--"+canonical)
			continue
		}
		if hasValue {
			out = append(out, "--"+canonical+"="+value)
			continue
		}
		out = append(out, "--"+canonical)
	}
	return out
}

// onlyBrowserFlags reports whether every argument is a flag the browser
// understands, so a bare `agent-sessions --codex` still opens the UI while
// `agent-sessions --output json` does not.
func onlyBrowserFlags(args []string) bool {
	for _, a := range args {
		name := strings.TrimLeft(a, "-")
		if i := strings.IndexByte(name, '='); i >= 0 {
			name = name[:i]
		}
		if !browserFlags[name] {
			return false
		}
	}
	return true
}

// splitArgs separates flags from positional arguments anywhere in the command
// line.
//
// Go's flag package stops parsing at the first non-flag token, so
// `search "auth" --limit 2` would fold the flags into the query. Agents write
// flags after the operand at least as often as before it, and a query that
// silently absorbs `--limit 2` returns zero results with no error — a failure
// mode worth eliminating rather than documenting.
func splitArgs(args []string) (flags, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]

		if a == "--" { // everything after is positional, by convention
			positional = append(positional, args[i+1:]...)
			return flags, positional
		}

		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}

		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if idx := strings.IndexByte(name, '='); idx >= 0 {
			continue // --flag=value carries its own operand
		}
		if boolFlags[name] {
			continue
		}
		// A value flag consumes the next token, even if it looks positional.
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return flags, positional
}
