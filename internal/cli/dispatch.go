package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/LeonKohli/claude-sessions/internal/index"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

// Commands lists the non-interactive surface, in the order `schema` reports it.
var Commands = []CommandDoc{
	{"list", "List sessions, newest first", "agent-sessions list --agent codex --limit 5"},
	{"search", "Find sessions whose transcript contains a query", `agent-sessions search "auth middleware"`},
	{"show", "Read conversation turns, optionally matching a literal query", "agent-sessions show 019fa425 experiments.md --limit 10"},
	{"calls", "Find recorded tool inputs in a session (literal, case-insensitive query)", "agent-sessions calls 019fa425 pcap-dir --limit 5"},
	{"files", "List files a session changed, and whether content is recoverable", "agent-sessions files 019fa425"},
	{"cat", "Write one file's stored content from a session to stdout", "agent-sessions cat 019fa425 lib/links.ts"},
	{"diff", "Print the unified diffs Codex recorded for a file", "agent-sessions diff 019fa425 lib/links.ts"},
	{"resume", "Print the command that reopens a session", "agent-sessions resume 019fa425"},
	{"schema", "Describe commands, flags, and error codes as JSON", "agent-sessions schema"},
}

// CommandDoc is one entry in the introspection output.
type CommandDoc struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Example string `json:"example"`
}

// Run executes a subcommand. It returns the process exit code, and reports
// whether the caller should instead launch the interactive TUI.
func Run(args []string) (exit int, runTUI bool) {
	if len(args) == 0 {
		// No subcommand: a terminal gets the browser, a pipe gets data. An
		// agent invoking the bare binary must never land in a TUI.
		if IsTTY(os.Stdout) {
			return 0, true
		}
		args = []string{"list"}
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "list", "search", "show", "calls", "files", "cat", "diff", "resume", "schema":
	case "-h", "--help", "help":
		usage(os.Stdout)
		return 0, false
	default:
		if strings.HasPrefix(cmd, "-") {
			// Flags with no subcommand. The browser understands --claude and
			// --codex; anything else is a request for structured output, and
			// honouring it matters more than the interactive default — asking
			// for --output json must never open a UI.
			if IsTTY(os.Stdout) && onlyBrowserFlags(args) {
				return 0, true
			}
			cmd, rest = "list", args
			break
		}
		return Fail(FormatAuto, cmd, CodeUsage, fmt.Sprintf("unknown command %q", cmd),
			"run `agent-sessions schema` to list commands"), false
	}

	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	// The flag package prints its own error and a full usage block to stderr.
	// A consumer reading stderr as JSON would hit that first, so discard it and
	// report the failure through the envelope instead.
	fs.SetOutput(io.Discard)

	var (
		output    = fs.String("output", "auto", "output format: auto, json, text")
		agentFlag = fs.String("agent", "", "restrict to an agent: claude or codex")
		project   = fs.String("project", "", "only sessions whose path contains this")
		since     = fs.Duration("since", 0, "only sessions modified within this window, e.g. 72h")
		subagents = fs.Bool("subagents", false, "include spawned subagent threads")
		limit     = fs.Int("limit", 0, "maximum results (0 uses the per-command default)")
		maxChars  = fs.Int("max-chars", DefaultMaxChars, "truncate each text field to this many characters")
		snippets  = fs.Int("snippets", DefaultSnippets, "context snippets per matching session")
	)
	// Accept the TUI's spellings so one habit works across both surfaces.
	claudeOnly := fs.Bool("claude", false, "shorthand for --agent claude")
	codexOnly := fs.Bool("codex", false, "shorthand for --agent codex")

	named := make(map[string]string)
	for _, name := range []string{"id", "query", "path"} {
		fs.Func(name, "named "+name+" operand", func(value string) error {
			if _, exists := named[name]; exists {
				return fmt.Errorf("duplicate --%s", name)
			}
			named[name] = value
			return nil
		})
	}
	flagArgs, positional := splitArgs(fs, rest)
	if err := fs.Parse(flagArgs); err != nil {
		if err == flag.ErrHelp {
			usage(os.Stdout)
			fs.SetOutput(os.Stdout)
			fs.PrintDefaults()
			return 0, false
		}
		format, formatErr := ParseFormat(*output)
		if formatErr != nil {
			format = FormatAuto
		}
		return Fail(format, cmd, CodeUsage, err.Error(), knownFlagsHint()), false
	}

	format, err := ParseFormat(*output)
	if err != nil {
		return Fail(FormatAuto, cmd, CodeUsage, err.Error(), ""), false
	}
	if *limit < 0 || *limit == math.MaxInt || *maxChars < 0 || *snippets < 0 || *since < 0 {
		return Fail(format, cmd, CodeUsage, "limits and durations must be nonnegative; --limit must be less than the maximum integer", ""), false
	}

	*agentFlag = ProviderFilter(*agentFlag, *claudeOnly, *codexOnly)
	if *agentFlag != "" && *agentFlag != "claude" && *agentFlag != "codex" {
		return Fail(format, cmd, CodeUsage,
			fmt.Sprintf("invalid --agent %q", *agentFlag), "want claude or codex"), false
	}

	positional, err = commandOperands(cmd, positional, named)
	if err != nil {
		return Fail(format, cmd, CodeUsage, err.Error(), ""), false
	}
	f := Filter{
		Provider: *agentFlag, Project: *project, Since: *since,
		Subagents: *subagents, Limit: *limit,
	}
	res, err := execute(cmd, positional, f, *snippets, *maxChars)
	if err != nil {
		return Fail(format, cmd, classify(err), err.Error(), hintFor(cmd, err)), false
	}
	if err := Emit(os.Stdout, format, cmd, res); err != nil {
		return Fail(format, cmd, CodeInternal, err.Error(), ""), false
	}
	return 0, false
}

func execute(cmd string, args []string, f Filter, snippets, maxChars int) (Result, error) {
	var entry session.SessionEntry
	switch cmd {
	case "show", "calls", "files", "cat", "diff", "resume":
		if len(args) == 0 {
			return Result{}, &Fault{Code: CodeUsage, Message: fmt.Sprintf("%s needs a session id", cmd)}
		}
		if (cmd == "cat" || cmd == "diff") && len(args) < 2 {
			return Result{}, &Fault{Code: CodeUsage, Message: fmt.Sprintf("%s needs a session id and a file path", cmd)}
		}
		var err error
		entry, err = Resolve(args[0], f.Provider)
		if err != nil {
			return Result{}, err
		}
	}
	switch cmd {
	case "schema":
		return schemaResult(), nil

	case "list":
		if f.Limit == 0 {
			f.Limit = DefaultListLimit
		}
		return List(f)

	case "search":
		if len(args) == 0 {
			return Result{}, &Fault{Code: CodeUsage, Message: fmt.Sprintf("search needs a query")}
		}
		if f.Limit == 0 {
			f.Limit = DefaultSearchLimit
		}
		return Search(strings.Join(args, " "), f, snippets, maxChars)

	case "show":
		limit := f.Limit
		if limit == 0 {
			limit = DefaultShowMessages
		}
		return Show(entry, strings.Join(args[1:], " "), limit, maxChars)

	case "files":
		return Files(entry)

	case "calls":
		if f.Limit == 0 {
			f.Limit = DefaultSearchLimit
		}
		return Calls(entry, strings.Join(args[1:], " "), f.Limit, maxChars)

	case "cat":
		return Cat(entry, args[1])

	case "diff":
		return Diff(entry, args[1])

	case "resume":
		return Resume(entry)
	}
	return Result{}, &Fault{Code: CodeUsage, Message: fmt.Sprintf("unknown command %q", cmd)}
}

// classify maps an error to a stable machine-readable code.
func classify(err error) string {
	if errors.Is(err, index.ErrStoreUnavailable) || errors.Is(err, session.ErrHistoryUnavailable) {
		return CodeUnavailable
	}
	var fault *Fault
	if errors.As(err, &fault) {
		return fault.Code
	}
	switch {
	case errors.Is(err, session.ErrFileNotTracked):
		return CodeNotFound
	case errors.Is(err, session.ErrNotRecoverable), errors.Is(err, session.ErrDiffUnsupported):
		return CodeUnrecoverable
	default:
		return CodeInternal
	}
}

func hintFor(cmd string, err error) string {
	switch classify(err) {
	case CodeAmbiguous:
		return "use the full id from `agent-sessions list` or `search`, including any rollout suffix"
	case CodeNotFound:
		if cmd == "cat" || cmd == "diff" {
			return "run `agent-sessions files <id>` to see the paths this session touched"
		}
		return "run `agent-sessions list` to see available sessions"
	case CodeUnrecoverable:
		if cmd == "cat" {
			return "codex records updates as diffs only — try `agent-sessions diff <id> <path>`"
		}
		return ""
	default:
		return ""
	}
}

func schemaResult() Result {
	data := map[string]any{
		"tool":    "agent-sessions",
		"schema":  SchemaVersion,
		"summary": "Search, read, and recover files from Claude Code and Codex session history.",
		// Declares the piped default so a consumer reads the contract rather
		// than inferring it, per CLI Spec principle 1.
		"output":   map[string]string{"tty": "text", "piped": "json"},
		"commands": Commands,
		"response_fields": map[string][]string{
			"list":   {"data.sessions", "data.count"},
			"search": {"data.hits", "data.count", "data.query"},
			"show":   {"data.session", "data.query", "data.turns"},
			"calls":  {"data.session", "data.query", "data.calls", "data.count"},
			"files":  {"data.session", "data.files", "data.count"},
			"diff":   {"data.session", "data.path", "data.diffs"},
			"resume": {"data.session", "data.cwd", "data.cwd_exists", "data.command"},
		},
		"raw_output_commands": []string{"cat"},
		"named_operands":      map[string]string{"--id": "session id", "--query": "search or filter text", "--path": "file path"},
		"provider_flags":      "A single --claude or --codex overrides --agent; both flags select both providers.",
		"identity":            "Pass returned ids unchanged. Codex archive ids are thread-id/rollout-id; resume commands use the logical thread id.",
		"provenance":          "Codex turns and calls include source (physical transcript path) and line. Referenced ancestors are bounded by their recorded cutoffs.",
		"limits": map[string]string{
			"truncated": "Omitted records, not shortened text. total is absent when not counted.",
			"text":      "show marks text_truncated; calls marks input_truncated. Search snippets are previews.",
			"zero":      "--limit 0 selects defaults; --max-chars 0 returns full show text or call input, but search snippets remain previews.",
		},
		"error_codes": []string{CodeUsage, CodeNotFound, CodeAmbiguous, CodeUnavailable, CodeUnrecoverable, CodeInternal},
		"global_flags": []map[string]string{
			{"flag": "--output", "values": "auto|json|text", "default": "auto"},
			{"flag": "--agent", "values": "claude|codex", "default": ""},
			{"flag": "--project", "values": "<substring>", "default": ""},
			{"flag": "--since", "values": "<duration>", "default": ""},
			{"flag": "--limit", "values": "<int>", "default": "per-command"},
			{"flag": "--max-chars", "values": "<int>", "default": fmt.Sprint(DefaultMaxChars)},
			{"flag": "--snippets", "values": "<int>", "default": fmt.Sprint(DefaultSnippets)},
			{"flag": "--subagents", "values": "bool", "default": "false"},
		},
		"agents": []map[string]string{
			{"name": "claude", "store": "~/.claude/projects", "resume": "claude --resume <id>"},
			{"name": "codex", "store": "~/.codex/sessions", "resume": "codex resume <id>"},
		},
		"recovery": map[string]string{
			"claude": "readable checkpoints or confirmed Write results; files reports recovery_source and recovery_earlier_version when newer checkpoints are unavailable",
			"codex":  "adds and deletes are recoverable when content was recorded; updates store only a unified diff",
		},
	}
	return Result{
		Data: data,
		Text: func(w io.Writer) {
			p := printer{w}
			p.line("agent-sessions — session history for Claude Code and Codex")
			p.blank()
			for _, c := range Commands {
				p.line("  %-8s %s", c.Name, c.Summary)
			}
			p.blank()
			p.line("Run with --output json for machine-readable output.")
		},
	}
}

func usage(w *os.File) {
	p := printer{w}
	p.line("agent-sessions — browse and search Claude Code and Codex sessions")
	p.blank()
	p.line("  agent-sessions                 interactive browser (terminal only)")
	for _, c := range Commands {
		p.line("  agent-sessions %-8s    %s", c.Name, c.Summary)
	}
	p.blank()
	p.line("Flags: --output json|text  --agent claude|codex  --limit N  --since 72h")
	p.line("Full contract: agent-sessions schema --output json")
}

// knownFlagsHint lists the accepted flags, so a bad spelling is recoverable in
// the same turn rather than costing a round trip to `schema`.
func knownFlagsHint() string {
	return "accepted flags: --output json|text, --agent claude|codex, --project, " +
		"--since, --limit, --snippets, --max-chars, --subagents"
}
