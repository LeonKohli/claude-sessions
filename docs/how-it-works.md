# How agent-sessions works

This page explains how `agent-sessions` finds sessions, how search stays fast, and why the CLI behaves the way it does.

## One binary, two interfaces

The bare command opens a browser. Any subcommand, or piped output, runs non-interactively and exits. The split follows principle 4 of the [CLI Spec](https://clispec.dev): a tool that opens a TUI when an agent calls it blocks the agent, so the agent cannot use it.

The CLI returns JSON with bounded size because an agent pays for every token it reads. A raw `rg` over the stores did not finish within 120 seconds on a 3 GB store and returns an unbounded result. `search --limit 5` returns a few kilobytes and says in `truncated` how many sessions it left out. The agent can then decide whether a partial answer is enough.

The CLI also accepts flag spellings from other tools, such as `--json` and `--max_results`. Agents reuse the spellings of the last tool they saw. A rejected spelling costs the agent a full turn, while an alias costs one map entry in `internal/cli/args.go`. Flags can follow operands for the same reason. Go's `flag` package stops at the first operand, so `search "auth" --limit 2` used to search for the literal text `auth --limit 2`.

## Finding Claude Code sessions

Claude Code stores transcripts under `~/.claude/projects/<encoded-dir>/`, or under `CLAUDE_CONFIG_DIR` when it is set. A `sessions-index.json` file describes some sessions. For the rest, the reader takes the title and working directory from the first 100 transcript lines.

The project path comes from the first available source in this order:

1. `originalPath` in `sessions-index.json`
2. `projectPath` of the session's entry in that file
3. The first non-null `cwd` in the transcript
4. The decoded directory name

The last source is a guess. Claude encodes `/` as `-` in directory names, so `client-app/ui-components` decodes to `client/app/ui/components`. Transcripts often start with records whose `cwd` is null, so the reader keeps scanning until it finds a real `cwd`. Stopping at the first record would fall through to the guess, and `resume` would print a `cd` into a directory that does not exist.

Subagent transcripts live in `<parent>/subagents/agent-*.jsonl`. Claude reuses agent IDs under different parents, so the tool names them `<parent>/agent-<id>`. Subagent sessions are hidden by default and cannot be resumed.

## Finding Codex sessions

Codex stores rollouts under `~/.codex/sessions/` and `~/.codex/archived_sessions/`, or under `CODEX_HOME`. Discovery first reads Codex's own thread database, the highest-numbered `state_<n>.sqlite`. It is the database `codex resume` uses, so titles, archived state, and token totals match what Codex shows. The tool opens it read-only and checks its columns with `PRAGMA table_info`, because the schema changes between Codex releases. Untitled threads get their first prompt from the transcript.

Without a usable database, discovery scans the rollout files instead. A selected rollout that the database lists but that is missing is an error. Falling back to an older rollout would return the wrong history.

A Codex thread can continue from an earlier rollout through `history_base`. Readers follow these references across both directories and respect each ancestor's byte and ordinal cutoffs. Conversation, tool calls, file recovery, and metrics all read the same history. A missing, ambiguous, or compressed ancestor is an error, not an empty history.

## Session IDs

Listings show 12-character ID prefixes instead of the usual 8. Codex IDs are UUIDv7 values that start with a millisecond timestamp, so sessions started within about a minute of each other share their first 8 characters, and every Claude subagent ID starts with `agent-`. On the author's store, an 8-character prefix was ambiguous for 64% of Claude sessions and 37% of Codex sessions. A 12-character prefix was unique for all of them.

## The search index

Search reads from a SQLite database with an FTS5 trigram index. The database lives in Go's [user cache directory](https://pkg.go.dev/os#UserCacheDir): `~/Library/Caches/agent-sessions/index.sqlite` on macOS, and `$XDG_CACHE_HOME/agent-sessions/index.sqlite` on Linux, which defaults to `~/.cache`. The file has mode `0600` and holds rebuildable metadata and conversation text, including source paths and line numbers.

A trigram index finds any substring of three or more characters, which matches how people search transcripts: file names, error messages, and flags. Each FTS candidate then gets an exact byte comparison against the lowercased text. Queries shorter than three characters, or queries that contain NUL, scan the stored text instead.

**Refreshing.** Every search compares the size and modification time of each selected transcript with the stored signature. A Codex signature covers every referenced ancestor. Only changed transcripts are parsed again, and only their new, changed, or removed messages touch the full-text index.

**The first build.** The first search into an empty index parses all transcripts on parallel workers. One writer stores the messages in large transactions, and SQLite then builds the full-text index in one pass. One pass is about twice as fast as updating the index message by message. For 21 GB of transcripts the first search takes about 30 seconds, and later searches take under a second. If the first build stops early, the next run finishes the full-text index before it searches.

**Parsing.** Tool output makes up about 91% of Claude transcript bytes and 98% of Codex rollout bytes. The reader locates JSON values without decoding them and decodes only the fields it keeps. Lines that are not conversation messages are skipped without full JSON validation.

**Concurrency.** Discovery and search take an OS file lock in the cache directory, so only one operation runs at a time across processes. A waiting process gives up after five seconds with `store_unavailable`. The binary limits Go to two CPUs, or fewer when `GOMAXPROCS` asks for fewer. There is no daemon and no file watcher. Indexing happens inside the command that needs it.

The database uses write-ahead logging, so `index.sqlite-wal` and `index.sqlite-shm` can appear next to it. To rebuild the index from scratch, delete the three files while no `agent-sessions` process runs.

## Compared with cass

[cass](https://github.com/Dicklesworthstone/coding_agent_session_search) covers 23 agents, keeps a persistent Tantivy index, and offers lexical, semantic, and hybrid search. If you need many agents or semantic search, use cass. The `--json` and `--robot` conventions come from cass.

`agent-sessions` covers two agents and adds file recovery. `files`, `cat`, and `diff` return a file as a past session recorded it, which cass does not reconstruct. It also reads Codex's own thread database instead of only the rollout files, and it needs no embeddings, model downloads, or daemon.
