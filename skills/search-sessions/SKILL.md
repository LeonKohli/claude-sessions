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
4. Responses are `{"ok":bool,"schema":1,"cmd":...,"data":...}`. Errors go to stderr with `ok:false` and a stable `error.code`; exit status is non-zero.
5. A `truncated` object means the answer is **partial** — say so, or re-run with a higher `--limit`.

## Commands

```bash
agent-sessions search "<query>" --json --limit 5   # find sessions, ranked
agent-sessions show <id>        --json --limit 20  # read the conversation
agent-sessions files <id>       --json             # what it changed on disk
agent-sessions cat <id> <path>                     # recover file content (raw bytes)
agent-sessions diff <id> <path> --json             # unified diffs (Codex updates)
agent-sessions resume <id>      --json             # command to reopen it
agent-sessions list             --json --limit 10  # recent sessions
agent-sessions schema           --json             # full contract, error codes, flags
```

Flags: `--agent claude|codex`, `--project <substring>`, `--since 72h`, `--limit N`, `--snippets N`, `--max-chars N`, `--subagents`. Flags may go before or after the operand.

Read `agent-sessions schema --json` rather than guessing — it lists every command, flag and error code.

## Workflow

**Finding something.** Start narrow, widen only if needed:

```bash
agent-sessions search "webhook signature" --json --limit 5 --snippets 2
```

Hits are ranked by match count and carry `id`, `agent`, `title`, `project`, `matches`, `snippets`, and a ready `resume` string. Scope with `--agent` or `--project` when the user names one. Pick the session from the snippets, then read it:

```bash
agent-sessions show <id> --json --limit 20 --max-chars 400
```

**Recovering a file.** `files` reports what a session touched and whether the content is retrievable:

```bash
agent-sessions files <id> --json
```

Each entry has `kind` (`add`/`update`/`delete`), `revisions`, and `recoverable`. When `recoverable` is true, `cat` writes the stored bytes to stdout with no envelope, so it redirects straight to a file. A bare filename works when unambiguous within that session.

```bash
agent-sessions cat <id> lib/links.ts > /tmp/old-links.ts
```

What is recoverable differs by agent, and this is not a tool limitation:

- **Claude Code** snapshots file contents, so any tracked revision returns byte-exact.
- **Codex** journals patches. Adds and deletes store the file verbatim; **updates store only a unified diff**. For those, `cat` fails with `not_recoverable` — use `diff`.

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
