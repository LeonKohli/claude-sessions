---
name: search-sessions
description: Search past Claude Code and Codex sessions. Use to find an earlier conversation, recall what was discussed or decided, locate a command or code change from a previous session, recover a file as an earlier session left it, or get the command that resumes a session.
argument-hint: "[search query]"
compatibility: Requires agent-sessions on PATH. Supports local Claude Code and Codex stores on macOS and Linux.
allowed-tools: Bash(agent-sessions:*) Bash(${CLAUDE_SKILL_DIR}/scripts/search.sh:*)
---

# Search agent sessions

`agent-sessions` indexes Claude Code and Codex transcripts and answers with bounded JSON. Reach the archives through it: transcript files under `~/.claude` and `~/.codex` reach hundreds of megabytes, and raw `grep` or the Read tool returns unbounded output.

## Rules

1. Always pass a subcommand. The bare command opens an interactive browser in a terminal.
2. Pass `--json` and read fields at the paths `agent-sessions schema --json` lists under `data.response_fields`: `search` returns `data.hits`, `show` returns `data.turns`, `calls` returns `data.calls`. A missing field is an error, not an empty list.
3. Treat a `truncated` object as a partial answer. Say so, or rerun with a higher `--limit`.
4. Shape output with native flags (`--limit`, `--max-chars`, `--snippets`) and `jq` projections that keep `truncated`. Keep JSON whole.
5. Pass each session `id` exactly as returned. A Codex `id` like `<thread-id>/<rollout-id>` selects one physical history.

## Steps

1. **Find the session.** Search for a distinctive literal term, such as a file name, an error message, or a flag:

	```bash
	agent-sessions search "webhook signature" --json --limit 5 --snippets 2
	```

	Queries are case-insensitive literal substrings, not word or semantic searches. Narrow with `--agent claude|codex`, `--project <substring>`, or `--since 72h` when the user names a provider, project, or time. `--since` takes Go durations, so 30 days is `720h`. The step is done when a hit's snippet matches what the user asked about. If nothing matches after two distinct terms, report that the search found nothing.

2. **Read the evidence.** Read the matching turns in full before you cite a decision or a measurement:

	```bash
	agent-sessions show <id> experiments.md --json --limit 20 --max-chars 0
	```

	`show` filters complete turns by the query, then applies `--limit`. A positive `--max-chars` returns excerpts marked `text_truncated`, and an excerpt can drop the caveat next to a number. Later turns can revise a decision in other words, so check the turns after the match before you call something final.

3. **Find a command or tool input**, when the evidence is something an agent ran:

	```bash
	agent-sessions calls <id> pcap-dir --json --limit 5 --max-chars 500
	```

	`calls` matches tool names, call IDs, and complete inputs. Fetch one complete input with `calls <id> <call-id> --json --max-chars 0`. A call is an attempt. Only its result proves success. Report recorded inputs and leave them unexecuted.

4. **Recover a file**, when the user wants an earlier version:

	```bash
	agent-sessions files <id> --json
	agent-sessions cat <id> lib/links.ts > /tmp/links.ts
	```

	`files` reports `kind`, `revisions`, and `recoverable` per file. Run `cat` only for `recoverable: true`. It writes raw bytes without an envelope. For a Codex `update`, `cat` returns `not_recoverable`, so run `agent-sessions diff <id> <path>` instead. If `recovery_earlier_version` is `true`, tell the user that a newer checkpoint is missing. Recover only recorded bytes, and state which content stays unavailable.

5. **Resume a session**, when the user wants to go back to it:

	```bash
	agent-sessions resume <id> --json
	```

	Give the user `cd <cwd> && <command>` from the result, and leave running it to them: resuming starts an interactive session. If `cwd_exists` is `false`, say that the directory is gone instead of giving a `cd` that fails.

## Evidence limits

- These are archive records, not the context an agent saw on resume. Generated summaries and injected instructions can have the role `user`. Check authorship before you quote a statement as the user's.
- Claude records come in raw file order, including abandoned branches.
- Codex turns and calls include `source` (the physical file) and `line`. Keep both when you cite evidence.
- An empty result does not prove a conversation never happened. Retention, filters, and unreadable transcripts can hide it.
- A file missing from `files` can still have changed. Shell commands and scripts write files without recording their content.

## Report

Lead with the answer. For each relevant session, give the agent, a short title, and the matching snippet on one line. Order by relevance, then recency. Add the resume command only when the user wants to go back. Summarize evidence in prose and quote short excerpts.

`scripts/search.sh [--claude|--codex] "<query>"` runs `agent-sessions search --json` for a query, and opens the browser without one.
