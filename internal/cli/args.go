package cli

import (
	"flag"
	"slices"
	"strings"
)

// flagAliases maps alternate spellings to canonical flags and fixed values.
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
	"q":           "query", "text": "query", "pattern": "query",
	"session": "id", "file": "path",
}

// browserFlags are the only flags the interactive browser accepts. Every other
// flag implies the caller wants the non-interactive surface.
var browserFlags = map[string]bool{"claude": true, "codex": true}

// splitArgs normalizes flags and separates interspersed operands in one pass.
// Option values and operands after -- remain literal.
func splitArgs(fs *flag.FlagSet, args []string) (flags, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			return flags, positional
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}

		name, value, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")

		if canonical, ok := flagAliases[name]; ok {
			a = "--" + canonical
			if strings.Contains(canonical, "=") {
				hasValue = true
			} else if hasValue {
				a += "=" + value
			}
			name = canonical
		}
		flags = append(flags, a)
		if hasValue || name == "h" || name == "help" {
			continue
		}
		if registered := fs.Lookup(name); registered != nil {
			if value, ok := registered.Value.(interface{ IsBoolFlag() bool }); ok && value.IsBoolFlag() {
				continue
			}
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return flags, positional
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

// ProviderFilter applies shorthand flags consistently in the CLI and browser.
// Both flags select all providers; a single shorthand overrides --agent.
func ProviderFilter(agent string, claudeOnly, codexOnly bool) string {
	switch {
	case claudeOnly && codexOnly:
		return ""
	case claudeOnly:
		return "claude"
	case codexOnly:
		return "codex"
	default:
		return agent
	}
}

func commandOperands(command string, positional []string, named map[string]string) ([]string, error) {
	if len(named) == 0 {
		return positional, nil
	}
	var roles []string
	switch command {
	case "search":
		roles = []string{"query"}
	case "show", "calls":
		roles = []string{"id", "query"}
	case "cat", "diff":
		roles = []string{"id", "path"}
	case "files", "resume":
		roles = []string{"id"}
	}
	for name := range named {
		if !slices.Contains(roles, name) {
			return nil, &Fault{Code: CodeUsage, Message: "--" + name + " is not an operand of " + command}
		}
	}
	var operands []string
	for _, role := range roles {
		if value, ok := named[role]; ok {
			operands = append(operands, value)
		} else if len(positional) > 0 {
			if role == "query" {
				operands = append(operands, strings.Join(positional, " "))
				positional = nil
			} else {
				operands = append(operands, positional[0])
				positional = positional[1:]
			}
		} else if role != "query" || command == "search" {
			return nil, &Fault{Code: CodeUsage, Message: command + " needs a " + role}
		}
	}
	if len(positional) > 0 {
		return nil, &Fault{Code: CodeUsage, Message: "unexpected operands for " + command}
	}
	return operands, nil
}
