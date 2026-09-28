package cli

import (
	"flag"
	"reflect"
	"strings"
	"testing"
)

// Agents write flags before or after the operand with equal frequency. Go's
// flag package stops at the first positional, which silently folded trailing
// flags into the search query and returned zero hits with no error.
func TestSplitArgs(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Bool("subagents", false, "")
	cases := []struct {
		name       string
		in         []string
		wantFlags  []string
		wantPosArg []string
	}{
		{
			name:       "flags after the operand",
			in:         []string{"unified diff", "--limit", "3", "--snippets", "1"},
			wantFlags:  []string{"--limit", "3", "--snippets", "1"},
			wantPosArg: []string{"unified diff"},
		},
		{
			name:       "flags before the operand",
			in:         []string{"--limit", "3", "unified diff"},
			wantFlags:  []string{"--limit", "3"},
			wantPosArg: []string{"unified diff"},
		},
		{
			name:       "flags on both sides",
			in:         []string{"--agent", "codex", "query", "--limit", "5"},
			wantFlags:  []string{"--agent", "codex", "--limit", "5"},
			wantPosArg: []string{"query"},
		},
		{
			name:       "equals form keeps its own value",
			in:         []string{"--limit=3", "query"},
			wantFlags:  []string{"--limit=3"},
			wantPosArg: []string{"query"},
		},
		{
			name:       "bool flag does not swallow the next token",
			in:         []string{"--subagents", "query"},
			wantFlags:  []string{"--subagents"},
			wantPosArg: []string{"query"},
		},
		{
			name:       "two operands with a bool flag between",
			in:         []string{"abc123", "--subagents", "lib/links.ts"},
			wantFlags:  []string{"--subagents"},
			wantPosArg: []string{"abc123", "lib/links.ts"},
		},
		{
			name:       "double dash forces the rest positional",
			in:         []string{"--", "--not-a-flag"},
			wantFlags:  nil,
			wantPosArg: []string{"--not-a-flag"},
		},
		{
			name:       "id and path for cat",
			in:         []string{"019fa404", "components/link-detail.tsx"},
			wantFlags:  nil,
			wantPosArg: []string{"019fa404", "components/link-detail.tsx"},
		},
	}

	for _, c := range cases {
		flags, pos := splitArgs(fs, c.in)
		if !reflect.DeepEqual(flags, c.wantFlags) {
			t.Errorf("%s: flags = %v, want %v", c.name, flags, c.wantFlags)
		}
		if !reflect.DeepEqual(pos, c.wantPosArg) {
			t.Errorf("%s: positional = %v, want %v", c.name, pos, c.wantPosArg)
		}
	}
}

func TestParseFormat(t *testing.T) {
	for _, ok := range []string{"auto", "json", "text"} {
		if _, err := ParseFormat(ok); err != nil {
			t.Errorf("ParseFormat(%q) errored: %v", ok, err)
		}
	}
	if _, err := ParseFormat("yaml"); err == nil {
		t.Error("ParseFormat accepted an unsupported format")
	}
}

// bound must report what it dropped: an agent that cannot tell a complete
// answer from a truncated one will treat the first page as the whole result.
func TestBoundReportsTruncation(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}

	got, trunc := bound(items, 2)
	if len(got) != 2 {
		t.Fatalf("returned %d items, want 2", len(got))
	}
	if trunc == nil {
		t.Fatal("truncation not reported")
	}
	if trunc.Returned != 2 || trunc.Total != 5 {
		t.Errorf("truncation = %+v, want returned 2 of 5", trunc)
	}

	if _, trunc := bound(items, 5); trunc != nil {
		t.Error("exact-fit result reported as truncated")
	}
	if _, trunc := bound(items, 0); trunc != nil {
		t.Error("unlimited result reported as truncated")
	}
}

func TestTruncateIsRuneSafe(t *testing.T) {
	if got := truncate("héllo wörld", 5); got != "héllo…" {
		t.Errorf("truncate = %q, want %q", got, "héllo…")
	}
	if got := truncate("short", 50); got != "short" {
		t.Errorf("truncate shortened a fitting string: %q", got)
	}
	if got := truncate("a\nb\nc", 0); got != "a b c" {
		t.Errorf("newlines not flattened: %q", got)
	}
}

func TestShortIDPrefersTheAgentHalf(t *testing.T) {
	// Qualified subagent ids are "<parent>/agent-xxxx"; showing the parent
	// prefix would make every sibling look identical.
	got := shortID("fe5755f1-0d89-47f3-b245-b3a14b7d5dd5/agent-a377140659a0437eb")
	if got != "agent-a37714" {
		t.Errorf("shortID = %q, want the agent half", got)
	}
	if got := shortID("019fa425-7964-7041-b895-b331deb81e89"); got != "019fa425-796" {
		t.Errorf("shortID = %q, want 12 chars", got)
	}
}

// Agents generalise flag spellings from other tools and from Python APIs.
// A rejected spelling costs a whole turn to recover from, so the common
// variants are normalised rather than refused.
func TestSplitArgsAcceptsCommonAgentSpellings(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"cass-style --json", []string{"--json"}, []string{"--output=json"}},
		{"cass-style --robot", []string{"--robot"}, []string{"--output=json"}},
		{"snake_case limit", []string{"--max_results", "5"}, []string{"--limit", "5"}},
		{"kebab variant", []string{"--max-results", "5"}, []string{"--limit", "5"}},
		{"snake_case max chars", []string{"--max_chars", "80"}, []string{"--max-chars", "80"}},
		{"provider alias", []string{"--provider", "codex"}, []string{"--agent", "codex"}},
		{"equals form preserved", []string{"--max_results=5"}, []string{"--limit=5"}},
		{"named query retains its role", []string{"--query", "auth"}, []string{"--query", "auth"}},
		{"named query with equals", []string{"--query=auth"}, []string{"--query=auth"}},
		{"named id retains its role", []string{"--id", "abc123"}, []string{"--id", "abc123"}},
		{"canonical flags untouched", []string{"--limit", "5"}, []string{"--limit", "5"}},
		{"unknown flag passes through to a real error", []string{"--bogus"}, []string{"--bogus"}},
		{"bare operand untouched", []string{"auth error"}, []string{"auth error"}},
	}
	for _, c := range cases {
		flags, positional := splitArgs(fs, c.in)
		got := append(flags, positional...)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: splitArgs(%v) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

// Named operands keep their roles alongside flag aliases.
func TestSplitArgsPreservesNamedOperandRoles(t *testing.T) {
	flags, pos := splitArgs(flag.NewFlagSet("test", flag.ContinueOnError), []string{"--query", "auth error", "--json", "--max_results", "3"})
	if len(pos) != 0 {
		t.Errorf("positional = %v, want named query to remain a flag", pos)
	}
	joined := strings.Join(flags, " ")
	for _, want := range []string{"--query auth error", "--output=json", "--limit 3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("flags %q missing %q", joined, want)
		}
	}
}
