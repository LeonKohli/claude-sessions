# agent-sessions

Search, read, and recover files from **Claude Code** and **Codex** session history — from a terminal or from a coding agent.

Both agents leave multi-GB piles of JSONL you can only get back into through their own cwd-scoped pickers. This indexes all of it and exposes it two ways: an interactive browser for you, and a structured, bounded CLI for the agent sitting next to you.

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go) ![Bubbletea](https://img.shields.io/badge/Bubbletea-TUI-FF75B5)

## Two surfaces

```bash
agent-sessions                                    # interactive browser (terminal only)
agent-sessions search "webhook retry" -o json     # everything else: non-interactive
```

Running it bare on a TTY opens the browser. Piped, or with any subcommand, it never blocks and never renders a UI — [CLI Spec](https://clispec.dev) principle 4. That distinction is the whole point: a tool that opens a TUI when an agent calls it is a tool an agent cannot use.

### For agents

| Command | Purpose |
|---|---|
| `search <query>` | Sessions matching a query, ranked by match count, with snippets |
| `show <id> [query]` | Conversation turns, optionally filtered by a literal substring |
| `calls <id> [query]` | Recorded tool inputs, optionally filtered by a literal substring |
| `files <id>` | What the session changed on disk, and whether content is recoverable |
| `cat <id> <path>` | Recovered file content, raw bytes, no envelope |
| `diff <id> <path>` | Unified diffs Codex recorded for a file |
| `resume <id>` | The command that reopens it, plus `cwd_exists` (prints, never execs) |
| `list` | Recent sessions |
| `schema` | Commands, response field paths, limits, flags, and error codes as JSON |

Flags: `--output auto\|json\|text`, `--agent claude\|codex`, `--project`, `--since 72h`, `--limit`, `--snippets`, `--max-chars`, `--subagents`.

Operands also accept names: `--id`, `--query`, and `--path`, where the command takes that operand. Their order does not matter: `show --query error --id <id>` filters the selected conversation. A single `--claude` or `--codex` overrides `--agent`; using both selects both providers in the CLI and browser.

JSON responses use an envelope, except successful `cat`, which returns raw bytes. The `truncated` field reports omitted records:

```json
{"ok":true,"schema":1,"cmd":"search","data":{"query":"error","hits":[…],"count":5},
 "truncated":{"returned":5,"total":848,"has_more":true,"hint":"raise --limit to see more (total 848)"}}
```

`total` appears only when the command counted the full set. `show` reports `has_more` without a `total`. Modern Codex and Claude previews stop after the requested window; legacy Codex rollouts are scanned fully to distinguish legacy events from duplicated response messages.

Text can also be shortened. `show` marks `text_truncated`, and `calls` marks `input_truncated`. Search snippets are always previews. See [storage and evidence limits](docs/storage-contract.md) before interpreting missing results, generated context, or file recovery as complete history.

Failures go to **stderr** with a stable code and a non-zero exit, so a consumer piping stdout never has to disentangle them from data:

```json
{"ok":false,"schema":1,"cmd":"cat","error":{
  "code":"not_recoverable",
  "message":"codex recorded links.ts as \"update\", which stores only a unified diff…",
  "hint":"try `agent-sessions diff <id> <path>`"}}
```

Codes: `usage_error`, `not_found`, `ambiguous_id`, `store_unavailable`, `not_recoverable`, `internal_error`.

`search` finds sessions by conversation text. `show` returns turns in transcript order. Add a case-insensitive literal query to filter complete turns before applying limits:

Use the returned `id` unchanged. Codex archive IDs use `<thread-id>/<rollout-id>` to distinguish physical histories, including the original rollout. A logical thread ID remains accepted when unambiguous. `resume` emits the provider's logical thread ID.

```bash
agent-sessions show <id> experiments.md --json --limit 20 --max-chars 0
```

Matching turns include their source `line`; Codex turns also include the physical `source` path. With a positive `--max-chars`, long matches return an excerpt around the first occurrence and set `text_truncated: true`. Use `--max-chars 0` to read complete matching turns before drawing conclusions from a decision or measurement. A query can miss later updates that use different wording; matching turns alone do not establish the final session state.

For a previous shell command or tool input, use `calls` on the selected session:

```bash
agent-sessions calls <id> pcap-dir --json --limit 5 --max-chars 500
```

The query matches call IDs, tool names, and complete inputs without regard to case. Filtering happens before limiting. Results retain transcript order and include the call ID, tool name, timestamp when recorded, source line, and an `input` string. Long inputs become excerpts around the match and set `input_truncated: true`. A call records an attempt; this command does not read results or establish success.

Use jq to select fields while retaining the partial-result warning:

```bash
agent-sessions calls <id> pcap-dir --json --limit 5 |
  jq '{truncated, calls: [.data.calls[] | {id,tool,source,line,input,input_truncated}]}'
```

To inspect a complete input, query its call ID with `--max-chars 0`. For a Claude `Bash` call, the complete `input` can then be decoded with `jq -r '.data.calls[].input | fromjson | .command'`. Custom tool inputs may be plain text instead of JSON. jq sees only returned records, so downstream filtering cannot find calls omitted by `--limit`. Keep JSON intact; use native limits and jq projections instead of `head -c`.

Search reports a failure if an indexed transcript cannot be read. It does not present an incomplete search as zero matches. Malformed JSON records are skipped so an unfinished final line does not prevent reading an active session.

### Why structured output rather than grep

Measured on this machine's store (3,961 sessions, ~3 GB), searching the term `error`:

| | Raw `rg` over the stores | `agent-sessions search --limit 5` |
|---|---|---|
| Wall clock | **did not finish in 120 s** | 4.2 s |
| Returned | unbounded | 3.4 KB (~855 tokens) |
| Coverage | unknown | `5 of 848`, declared |

Bounded output is not politeness. An unbounded result is one an agent cannot afford to read, which makes the history effectively unavailable.

### Argument handling

Agents generalise flag spellings from whatever tool they saw last, and a rejected spelling costs a whole turn to recover from. These all work:

```bash
agent-sessions search unified diff --limit 2     # unquoted multi-word query
agent-sessions search "unified" --json           # --json / --robot → --output json
agent-sessions search --query "unified"          # named operand lifted to positional
agent-sessions list --provider codex             # --provider / --tool → --agent
agent-sessions search "x" --max_results 5        # snake_case → --limit
```

Flags may appear before or after the operand. Go's `flag` package stops at the first positional, which silently folded trailing flags into the query — `search "auth" --limit 2` searched for the literal string `auth --limit 2` and returned nothing, with no error. Unknown flags produce a structured `usage_error` listing the accepted ones, with no usage block leaking onto stderr ahead of the JSON.

### Alternatives

[**cass**](https://github.com/Dicklesworthstone/coding_agent_session_search) is the serious tool in this space — 21k lines of Rust, 23 agents, a persistent Tantivy index, and lexical/semantic/hybrid search. If you want breadth or fast repeated search, use it. It is also where the `--json`/`--robot` convention and the "never run bare in an agent context" warning come from.

Where this tool differs:

- **File recovery.** `files`/`cat`/`diff` retrieve a file as it existed in a past session. cass indexes conversations; it does not reconstruct file content.
- **Codex's own thread index.** Discovery reads `state_<n>.sqlite` — the database `codex resume` itself trusts — so titles, archived state and token totals match what Codex reports. Other tools read the rollout JSONL only.
- **Scope.** Two agents, no embeddings, no daemon, no model downloads.

The honest gap is search cost. This greps every transcript per query at ~500 MB/s: **~4 s over 3 GB, ~12 s over 6 GB with subagents.** A prebuilt inverted index answers in milliseconds. That is fine for occasional recall and wrong for a tight loop — if you start calling `search` repeatedly, use cass.

### For humans

| Key | Action |
|-----|--------|
| `↑/k` `↓/j`, `g`/`G`, `Ctrl+u`/`Ctrl+d` | Navigate |
| `/` | Search sessions · `Ctrl+s` titles/metadata ↔ transcript · `↑↓` select while typing |
| `Tab` | Focus the list or preview |
| `s` `p` `d` | Choose sort order / project / date range from a searchable list |
| `f` | Choose an agent filter (all, Claude, Codex) |
| `a` | Show/hide subagent threads |
| `o` | Browse recorded files; `Tab` focus list/content, `↑↓` navigate, `PgUp/PgDn` scroll, `Esc` return |
| `[` / `]` | Scroll the conversation preview |
| `Ctrl+k` / `F1` | Open actions for the current view; type to filter, `↑↓` select, `Enter` apply, `Esc` cancel |
| `?` | Open actions when not editing a search field |
| `Enter` | Read the selected conversation; `/` find text, `n`/`N` next/previous match, `Esc` return |
| `r` | Resume · `y` copy id · `q` quit |

Actions stay available while editing a query and in the conversation and file views. Canceling keeps the previous query, selection, and focus. Navigation acts on the focused pane; `g/G` in the preview scrolls it without changing the selected session.

Each two-line entry starts with a persistent Claude or Codex badge, followed by its title and project/date metadata. Provider colors stay separate from the selected-entry highlight. The palette adapts to light and dark terminals; names and selection markers remain visible without color. Long queries scroll inside the search field. Below 90 columns, the list sits above the preview. The browser needs at least 40 columns and 18 rows.

The preview and reader distinguish literal user prompts from Markdown answers, including headings, emphasis, lists, links, and syntax-highlighted code. In the reader, `m` switches between Markdown and source text, preserving original code indentation in source mode. Search works on the displayed text. The reader loads beyond the 30-message preview and stops after the message that crosses 8 MiB of conversation text; use `show` for the remaining records. Returning keeps the search and selected session. These views contain conversation text; they do not yet display tool activity or reasoning blocks.

The TUI uses Bubble Tea 2, Bubbles 2, and Lip Gloss 2 and requires Go 1.26 or later to build. Scrolling and reader search highlights use Bubbles viewports. Conversation formatting runs in the background, so you can leave the reader while a long conversation loads. Switching back from source reuses the last Markdown layout when the width and theme still match.

The file browser is read-only. It distinguishes historical file content from recorded diffs and shows recovery provenance. These records do not establish the final filesystem state. File previews show up to 64 KiB; use `cat` or `diff` for the complete record.

## File recovery

`files` reports `kind` (`add`/`update`/`delete`), `revisions`, and `recoverable`. Claude results also identify `recovery_source` as `claude_checkpoint` or `claude_write`. `recovery_earlier_version: true` means a newer recorded checkpoint is unavailable and `cat` returns an earlier version. Check this metadata before restoring a file.

| | Claude Code | Codex |
|---|---|---|
| Model | snapshots **state** | journals **transitions** |
| Store | checkpoint files and successful `Write` results in the transcript | inline in the rollout |
| Added / deleted files | available checkpoint bytes | recorded content, when present |
| Modified files | latest recoverable recorded content | **unified diff only** |

Claude recovery selects the latest readable checkpoint or confirmed `Write` result in transcript order. Repeated references to the same checkpoint count once. A `Write` result must match its tool call and report success; pending or failed requests are excluded. These bytes are a historical version, not necessarily the session's final file.

[Claude checkpoints](https://code.claude.com/docs/en/checkpointing) exclude shell edits and expire with session retention. Python, shell scripts, MCP tools, and other programs may change files without recording their bytes. This tool does not execute logged code or infer file contents from commands. Successful Claude `Edit` results can contain captured originals, but reconstructing those edits is not currently supported; their checkpoints remain usable.

Codex recovery uses successful patch records. Failed or declined changes are excluded, and an empty stored file is recoverable. Updates contain a unified diff rather than complete file contents, so `cat` returns `not_recoverable` and points at `diff`.

## How it works

### Claude Code

`~/.claude/projects/<encoded-dir>/` (honours `CLAUDE_CONFIG_DIR`). A `sessions-index.json` covers some sessions; the rest are read from the first 100 transcript lines, including user messages and working-directory metadata. The JSONL entry format is internal to Claude Code, not a stable public contract.

Project paths resolve in order: `originalPath` from a `sessions-index.json` → its `projectPath` → the `cwd` recorded in the transcript → the decoded directory name.

That last step is a guess and must stay one. Directory names encode `/` as `-` (`-Users-example-projects` → `/Users/example/projects`), which is irreversible for any project whose own name contains a hyphen: `client-app/ui-components` decodes to `client/app/ui/components`. Transcripts open with `last-prompt`/`mode`/`permission-mode` records whose `cwd` is null, so reading it from line 0 alone yields nothing and drops straight through to that guess — which is how `resume` ends up emitting a `cd` into a directory that never existed. The reader therefore keeps scanning until it finds a non-null `cwd`.

Tracked file paths are recorded relative to the project and re-anchored with `realParentDir`.

Subagent transcripts (`<parent>/subagents/agent-*.jsonl`) are indexed but hidden. Claude reuses the same agent id under different parents, so their ids are qualified as `<parent>/agent-<id>`. They are not resumable, and the tool says so rather than emitting a command that fails.

### Codex

Rollouts live under `~/.codex/sessions/` and `archived_sessions/` (honours `CODEX_HOME`). Older files commonly use `YYYY/MM/DD/rollout-<ISO>-<uuid>.jsonl`. Reverted threads can retain multiple physical rollouts; their archive IDs distinguish them. Readers follow `history_base` references across both directories, preserving each ancestor's byte and ordinal cutoffs. Conversation, calls, recovery, and preview metrics use that same history. Missing, ambiguous, compressed, or invalid referenced history fails explicitly. Lines use `{timestamp, type, payload}`:

| Line | Carries |
|------|---------|
| `session_meta` (first) | thread id, cwd, git branch, cli version, `thread_source` |
| `turn_context` | model, reasoning effort, sandbox policy |
| `event_msg` / `user_message`, `agent_message` | the conversation |
| `response_item` / `message` | user `input_text` and assistant `output_text`; preferred over duplicate legacy events |
| `event_msg` / `token_count` | **running totals** — the last one wins, they are not summed |
| `event_msg` / successful `patch_apply_end` | `changes`: content for add/delete, `unified_diff` for update |
| `event_msg` / `item_completed` with completed `FileChange` | newer patch records with the same change data |
| `response_item` / `function_call`, `custom_tool_call` | tool names |

Discovery prefers Codex's own SQLite thread index (`~/.codex/state_<n>.sqlite`, the same one `codex resume` trusts), opened read-only with a busy timeout. It supplies title, token total, model, git branch and archived state without reading a transcript.

The highest-numbered `state_<n>.sqlite` wins. Columns are probed via `PRAGMA table_info`, and untitled rows get their prompt backfilled from the transcript. Missing legacy rollouts are skipped. A missing selected paginated rollout produces `store_unavailable`; substituting an older rollout would return the wrong history. An absent or unreadable database falls back to a parallel rollout scan.

### Session ids

Listings show a 12-character prefix, not the conventional 8. Codex ids are UUIDv7 so sessions from the same period share a long prefix, and Claude subagent ids all begin `agent-`. Measured here, an 8-character prefix is ambiguous for **64% of Claude sessions and 37% of Codex ones**; 12 resolves every one. Commands accept any unambiguous prefix and return `ambiguous_id` otherwise.

### Caching

The gob index uses [Go's user cache directory](https://pkg.go.dev/os#UserCacheDir): `~/Library/Caches/agent-sessions/index.gob` on macOS and `$XDG_CACHE_HOME/agent-sessions/index.gob` on Linux, defaulting to `~/.cache`. It is versioned and fingerprinted with its transcript stores and state database. Changing `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, or `CODEX_SQLITE_HOME` prevents reuse of another store's index. Transcript sizes and modification times invalidate stale metadata even when a session is appended without changing its parent directory. Cache files are private to the user.

The write is synchronous. Doing it in a background goroutine works fine for the browser and never completes for anything else: a subcommand exits before the goroutine runs, so every invocation re-scanned the whole store and left an orphaned `.tmp` behind. Encoding costs ~30 ms against a ~2 s scan.

**Cold ~2 s for ~4,000 sessions across both agents; warm ~40 ms.**

## Architecture

```
cmd/agent-sessions/main.go       # cli.Run → subcommand, or the browser on a TTY
internal/
  cli/                           # agent-facing surface
    dispatch.go                  # routing, flags, error classification, schema
    commands.go                  # list, search, show, files, cat, diff, resume
    output.go                    # envelope, TTY detection, stdout/stderr split
    args.go                      # flag/positional split (flags may follow operands)
  provider/provider.go           # Kind enum, store locations, resume argv
  session/
    types.go                     # SessionEntry, PreviewMessage, dispatch
    reader.go / enrich.go        # Claude transcripts
    codex.go                     # Codex rollouts
    files.go                     # file changes + recovery, both providers
  index/
    scanner.go                   # Claude store walk (parallel)
    codex.go                     # SQLite fast path + rollout fallback
    search.go                    # parallel transcript grep, shared by CLI and TUI
    cache.go / index.go          # gob cache, concurrent load
  tui/                           # browser
  util/                          # paths, formatting, clipboard
```

## Build & install

The same Go source builds on macOS and Linux. SQLite is pure Go and does not require a system library.

Tagged [releases](https://github.com/LeonKohli/claude-sessions/releases) package the `agent-sessions` binary and `skills/search-sessions/` together for macOS and Linux, on amd64 and arm64. Verify the archive against the release SHA256SUMS, extract it, and install the binary in a directory on your PATH. Install or link the bundled skill directory in your agent's skills directory. The skill requires the binary on PATH and does not download or build it.

For a source installation, use a published tag or exact commit:

```bash
go install github.com/LeonKohli/claude-sessions/cmd/agent-sessions@<version>
```

Or build from the checkout:

```bash
go build -ldflags="-s -w" -o agent-sessions ./cmd/agent-sessions
mv agent-sessions ~/.local/bin/
```

The browser copies IDs with macOS `pbcopy`, Wayland [`wl-copy`](https://github.com/bugaevc/wl-clipboard), or X11 `xclip`. Install `wl-clipboard` or `xclip` for the Linux desktop you use. A missing desktop or clipboard command produces a visible error. CLI commands do not require clipboard tools.

Transcript roots follow the documented [`CLAUDE_CONFIG_DIR`](https://code.claude.com/docs/en/env-vars) and [`CODEX_HOME`](https://developers.openai.com/codex/environment-variables). The Codex SQLite fast path also honours `CODEX_SQLITE_HOME`. The tool does not parse Codex's `sqlite_home` configuration option; if no database is found at the selected location, it reads the rollout files.

## Releases

CI tests on Linux and macOS with isolated transcript roots. A `v*` tag runs [GoReleaser](https://goreleaser.com/) to build the four platform archives and SHA256SUMS. All archives include the skill from the same commit as the binary.

To check packaging without publishing:

```bash
goreleaser check
goreleaser release --snapshot --clean
```

## Testing

```bash
go test ./...
```

`TestCommandsWithRelocatedStores` runs the public CLI dispatcher against synthetic Claude and Codex histories. It checks search, show, files, and resume with custom roots and a project path containing spaces. The tests also cover provider discovery and cache isolation.

Some existing index tests inspect local Codex history when it is available. To run only against fixtures, point both provider roots at empty temporary directories before running `go test ./...`.

## Dependencies

| Package | Purpose |
|---------|---------|
| [bubbletea](https://github.com/charmbracelet/bubbletea) / [bubbles](https://github.com/charmbracelet/bubbles) / [lipgloss](https://github.com/charmbracelet/lipgloss) | TUI |
| [modernc.org/sqlite](https://modernc.org/sqlite) | Codex thread index (pure Go, no cgo) |
| [golang.org/x/term](https://pkg.go.dev/golang.org/x/term) | TTY detection |

CLI commands require no external search tools or system SQLite. The browser's copy action needs the desktop clipboard command described above.
