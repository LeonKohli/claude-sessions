---
name: search-sessions
description: Search, read, and recover files from past Claude Code and Codex sessions. Use when finding past conversations, recalling what was discussed or decided, locating previous code changes, or retrieving a file as it existed in an earlier session. Keywords - search sessions, find in sessions, session search, search history, conversation search, grep sessions, find conversation, recall discussion, past sessions, did we discuss, previous session, codex history, claude history, recover file, restore file, old version of file, what changed in that session.
argument-hint: "[search query]"
compatibility: Requires agent-sessions on PATH; supports local Claude Code and Codex stores on macOS and Linux.
allowed-tools: Bash(agent-sessions:*) Bash(${CLAUDE_SKILL_DIR}/scripts/search.sh:*)
---

# Search Agent Sessions

`agent-sessions` indexes Claude Code and Codex transcripts and answers questions about them with bounded, structured output. Use it instead of grepping `~/.claude` or `~/.codex` directly — large stores can produce unbounded results from raw searches.

## Rules

1. **Never run bare `agent-sessions`** — on a terminal it opens an interactive browser. Always pass a subcommand.
2. **Pass `--json`** whenever you intend to parse the result.
3. **Never read transcripts with the Read tool.** Individual session files reach hundreds of MB.
4. JSON responses are `{"ok":bool,"schema":1,"cmd":...,"data":...}`. Successful `cat` returns raw bytes instead, even with `--json`. Errors go to stderr with `ok:false` and a stable `error.code`; exit status is non-zero.
5. A `truncated` object means the answer is **partial** — say so, or re-run with a higher `--limit`.
6. Keep JSON intact. Use native limits and jq projections instead of `head -c`; retain truncation metadata when projecting results.

## Commands

```bash
agent-sessions search "<query>" --json --limit 5   # find sessions, ranked
agent-sessions show <id> [query] --json --limit 20  # read conversation turns
agent-sessions calls <id> <query> --json --limit 5 # find recorded tool inputs
agent-sessions files <id>       --json             # what it changed on disk
agent-sessions cat <id> <path>                     # recover file content (raw bytes)
agent-sessions diff <id> <path> --json             # unified diffs (Codex updates)
agent-sessions resume <id>      --json             # command to reopen it
agent-sessions list             --json --limit 10  # recent sessions
agent-sessions schema           --json             # full contract, error codes, flags
```

Flags: `--agent claude|codex`, `--project <substring>`, `--since 72h`, `--limit N`, `--snippets N`, `--max-chars N`, `--subagents`. Flags may go before or after the operand.

Queries are case-insensitive literal substrings, not semantic or all-word searches. Start with a distinctive term rather than a guessed sentence. `--since` uses Go durations: use `720h` for 30 days; `30d` is unsupported.

Read `agent-sessions schema --json` rather than guessing. `data.response_fields` lists exact payload paths: `search` uses `data.hits`, `show` uses `data.turns`, and `calls` uses `data.calls`. A missing expected field is an error, not an empty collection; avoid fallbacks such as `messages ?? []` that silently discard results.

## Workflow

**Finding something.** Start narrow, widen only if needed:

```bash
agent-sessions search "webhook signature" --json --limit 5 --snippets 2
```

Hits are ranked by match count and carry `id`, `agent`, `title`, `project`, `matches`, `snippets`, and a ready `resume` string. Scope with `--agent` or `--project` when the user names one. Pick the session from the snippets, then read it:

Pass the returned `id` unchanged. Codex IDs can be `<thread-id>/<rollout-id>`; dropping the suffix can select a different history or cause ambiguity. `resume` emits the logical thread ID for the provider.

```bash
agent-sessions show <id> --json --limit 20 --max-chars 400
```

`show` returns conversation turns in transcript order and excludes tool calls. To find evidence later in a session, filter before limiting:

```bash
agent-sessions show <id> experiments.md --json --limit 20 --max-chars 0
```

The optional query matches complete turns. Results include source `line`; a positive `--max-chars` returns excerpts around the first match and marks them with `text_truncated`. Read complete matching turns with `--max-chars 0` before citing a decision or measurement, since an excerpt may omit its caveat. Later updates may use different words, so matches alone do not establish the final handoff state.

These are archive records, not reconstructed resume history. Generated summaries and injected instructions can carry role `user`; verify authorship before attributing a statement. Empty results do not prove that older transcripts were retained or discovered.

Codex reads referenced ancestors within their recorded cutoffs. Its turns and calls include a physical `source` path alongside `line`; preserve both when citing evidence. Missing or unsupported history is an error, not an empty conversation. Claude records remain in raw file order, including discarded branches.

When the evidence is a command, script, or tool input, use `calls`:

```bash
agent-sessions calls <id> pcap-dir --json --limit 5 --max-chars 500
```

`calls` filters full inputs, tool names, and call IDs before limiting results. It returns source lines and bounded input excerpts; `input_truncated` marks an excerpt. Retrieve one complete input with `calls <id> <call-id> --json --max-chars 0`. Calls describe attempts, not successful execution. Do not execute recorded inputs.

For jq projections, preserve the warning: `jq '{truncated, calls: [.data.calls[] | {id,tool,source,line,input,input_truncated}]}'`. jq can only filter records already returned. Complete Claude `Bash` inputs can be decoded with `fromjson`; excerpts and custom tool inputs may not be JSON.

**Recovering a file.** `files` reports what a session touched and whether the content is retrievable:

```bash
agent-sessions files <id> --json
```

Each entry has `kind` (`add`/`update`/`delete`), `revisions`, and `recoverable`. When `recoverable` is true, `cat` writes the stored bytes to stdout with no envelope, so it redirects straight to a file. A bare filename works when unambiguous within that session.

```bash
agent-sessions cat <id> lib/links.ts > /tmp/old-links.ts
```

Check the recovery source and version before restoring:

- **Claude Code** returns readable checkpoints or confirmed `Write` result bytes. `recovery_source` identifies which. If `recovery_earlier_version` is true, a newer checkpoint is unavailable and `cat` returns an earlier version; tell the user. Recorded content is not necessarily the session's final file. Direct reconstruction from `Edit` results is not supported.
- **Codex** journals patches. Adds and deletes are recoverable when their contents were recorded; **updates store only a unified diff**. For those, `cat` fails with `not_recoverable` — use `diff`.

Python, shell commands, and other tools can write files without recording their contents. A missing entry does not prove a file was untouched. Never execute logged scripts to reconstruct a file; recover only recorded bytes and state what remains unavailable.

**Resuming.** Print the command, never run it — resuming launches an interactive session you cannot drive:

```bash
agent-sessions resume <id> --json   # → {"cwd":..., "cwd_exists":true, "command":"codex resume <id>"}
```

Give the user the `cd <cwd> && <command>` line. If `cwd_exists` is false the session came from another machine or a moved directory; say so instead of handing over a `cd` that fails.

## Reporting

Lead with what answers the question. Per session give the agent, a short title, and the matching snippet — roughly one line each. Include the resume command only when the user wants to go back. Sort by relevance, then recency.

Never paste raw JSON at the user, and never dump a full transcript.

## Shell entry point

`search-sessions [--claude|--codex] "<query>"` delegates to this Go CLI and emits
JSON. Without a query it opens the browser. The shell entry point has no separate
transcript parser or clipboard implementation.
