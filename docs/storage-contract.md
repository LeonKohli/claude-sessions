# Session storage and evidence

`agent-sessions` reads local archives. Its output is not a reconstruction of the exact context an agent would receive on resume, or a complete filesystem journal.

This reference separates provider documentation from observed internal formats and the current reader's limits. Internal-format observations cover Claude Code 2.1.72 and 2.1.266, and Codex 0.157.1 and 0.158.0-alpha.2. These are compatibility examples, not a supported-version range.

## Storage and authority

| Store | Purpose | Reader |
|---|---|---|
| Claude `projects/<project>/<session>.jsonl` under `CLAUDE_CONFIG_DIR`, default `~/.claude` | Messages, tool records, and session metadata | `internal/session/reader.go`, `calls.go` |
| Claude `<session>/subagents/agent-*.jsonl` | Separate child-agent transcripts | `internal/index/scanner.go` |
| Claude checkpoint files referenced by `file-history-snapshot` | Historical file bytes, subject to retention | `internal/session/files.go` |
| Codex `sessions/` and `archived_sessions/` under `CODEX_HOME`, default `~/.codex` | Persisted rollout records | `internal/session/codex.go`, `calls.go` |
| Codex `state_<n>.sqlite` under `CODEX_SQLITE_HOME` or `CODEX_HOME` | Thread metadata and rollout locations | `internal/index/codex.go` |
| `agent-sessions/index.sqlite` in the OS user cache directory | Rebuildable metadata and conversation search text | `internal/index/database.go` |

Claude explicitly calls its JSONL entry format internal and warns that it can change between releases. Project directory encoding is not reversible: it replaces non-alphanumeric characters and can truncate long names or use a configured name. Recorded working directories take precedence over decoding directory names. Retention and disabled persistence can remove or prevent local history. See [Claude session storage](https://code.claude.com/docs/en/sessions).

The Codex database supplies discovery metadata and selects the current physical rollout. The fallback scans physical files when the database is absent, unreadable, or yields no entries. A missing selected paginated rollout is an error, so fallback cannot replace it with obsolete history. Discovery does not merge unindexed files into a nonempty database result.

## Raw records and resumed history differ

Claude records include message UUIDs, parent links, compaction boundaries, and generated summaries. A generated summary can have role `user`; that role alone does not establish human authorship. The SDK's store-backed `getSessionMessages` returns the linked post-compaction chain. A configured store adapter's `load` returns raw entries including earlier history; the application implements that adapter. The store does not mirror checkpoint files. See [Claude session-store semantics](https://code.claude.com/docs/en/agent-sdk/session-storage).

Our Claude reader emits text in file order without interpreting parent links or compaction flags. This preserves archive evidence but does not identify the selected branch. Summaries intentionally retain role `user` in both official SDKs. SDK readers also differ: TypeScript 0.3.283 restores preserved compaction segments and sibling response fragments that Python 0.2.160 omits. Matching a simple parent walk would not establish exact resume-context support.

Codex separates model-input `response_item` records from conversation events. Observed user-role response items include injected AGENTS instructions. Our reader can use those instructions as the first user turn and fallback title. Upstream's [history projection](https://github.com/openai/codex/blob/1b1835f751ebdc0cfc50b3fe55d4571dbb294563/codex-rs/app-server-protocol/src/protocol/thread_history.rs) treats these record families differently.

Codex archive IDs use `<thread-id>/<rollout-id>` for recognized rollout filenames. The original rollout repeats the thread ID as its rollout ID. These references keep search, retrieval, and browser previews on the same physical history. An unqualified logical ID resolves only when unambiguous; resume commands always use the logical ID.

Codex readers follow `history_base.thread_id` as a physical rollout reference, recursively reading ancestors before local records. Ancestor records are bounded by both `end_byte_offset` and `end_ordinal_exclusive`. Conversation, calls, file evidence, and enrichment share this traversal. `show` and `calls` retain physical `source` paths and line numbers. Missing or ambiguous ancestors, cycles, invalid cutoffs, and compressed `.zst` histories fail explicitly. See the pinned upstream [lineage rules](https://github.com/openai/codex/blob/1b1835f751ebdc0cfc50b3fe55d4571dbb294563/codex-rs/thread-store/src/local/rollout_lineage.rs) and [rollout selection](https://github.com/openai/codex/blob/1b1835f751ebdc0cfc50b3fe55d4571dbb294563/codex-rs/thread-store/src/local/thread_rollout_resolver.rs).

This reconstructs referenced archive ranges, not the provider's full conversation projection. Injected instructions remain visible, and legacy rollback markers are not replayed. Claude branch selection remains unsupported. Conversation selection must stay separate from filesystem state: a conversation-only rewind can retain file writes from the discarded branch.

## Attempts, results, and file bytes

`calls` returns attempted tool inputs. Execution success requires the corresponding result, not an assistant's statement or a tool-call name. File recovery accepts recognized successful writes or patch events and readable checkpoints. It does not execute recorded commands.

Claude checkpoints are separate from the transcript and can expire. The current documentation limits retained checkpoints and excludes Bash edits from checkpoint coverage. A transcript copy therefore need not contain recoverable file bytes. See [checkpointing](https://code.claude.com/docs/en/checkpointing). `recovery_source` identifies recorded provenance; `recovery_earlier_version` warns when recovery falls back. Neither proves the final file state. Codex updates expose diffs rather than complete files.

Claude usage can repeat across content fragments sharing one `message.id`. TUI enrichment counts each response once and uses its latest recorded usage, while collecting tools from all fragments. Records without response IDs retain per-record accounting. These are provider-reported usage totals, not measurements of retrieval savings. Headless retrieval output does not use this enrichment.

## CLI interpretation

| Command | Successful payload | Meaning |
|---|---|---|
| `search` | `data.hits`, `data.count` | Ranked discovered sessions with preview snippets |
| `show` | `data.turns` | Extracted conversation text in record order |
| `calls` | `data.calls`, `data.count` | Recorded tool inputs |
| `files` | `data.files`, `data.count` | Recognized file-change and recovery evidence |
| `cat` | Raw bytes, even with `--json` | A recoverable historical file; an empty file produces zero bytes |

A missing expected collection is a contract mismatch, not an empty result. `schema --json` lists payload paths by command under `data.response_fields`. `data.raw_output_commands` identifies exceptions such as `cat`. Failures use a nonzero exit status; JSON diagnostics go to stderr.

Envelope `truncated` describes omitted records. Search snippets are previews even without this flag. `show` marks shortened text with `text_truncated`; `calls` uses `input_truncated`. `--max-chars 0` returns complete turns or inputs, preserving whitespace; it does not make search snippets complete. CLI `--limit 0` selects defaults.

For search, a positive `--max-chars` bounds each snippet in Unicode characters, including its role label. The label is omitted when it leaves no room for content.

An empty search means no recognized matches among discovered sessions under the filters. Missing provider directories are allowed. Errors enumerating a selected provider's directories return `store_unavailable` instead of empty success. Individual unreadable or unrecognized transcript headers can still be skipped during discovery, and malformed records are skipped during parsing. Search checks file signatures, uses persisted text for unchanged transcripts, and propagates errors refreshing changed transcripts. Missing or ambiguous Codex ancestors still fail after indexing. Empty results therefore do not establish complete historical coverage.

Discovery and search each wait at most five seconds for the shared index lock. Lock contention beyond that wait returns `store_unavailable` with a retry hint. Changed transcripts are reread, but unchanged conversation messages retain their existing full-text index entries.

## Provider interfaces for comparison

The index owns discovery and cached metadata. The session package owns archive traversal, record interpretation, and recovery evidence. CLI and TUI conversation retrieval share `WalkSearchable`; `ReadPreview` only collects a bounded prefix. JSON decoding determines record types. Candidate filtering can skip irrelevant records but must admit escaped strings for decoding.

Claude's [Python SDK](https://code.claude.com/docs/en/agent-sdk/python) offers `list_sessions` and `get_session_messages` for saved local sessions. These are better comparison points for provider-interpreted history than another independently written JSONL parser. Resuming with a new prompt is a different operation and can create new evidence.

Codex documents read-only history retrieval through `thread/read` and paginated `thread/turns/list`. It also documents rollback markers and archival moves. See the [app-server protocol](https://developers.openai.com/codex/app-server). Generate the installed version's schema with `codex app-server generate-json-schema --out <temporary-directory>` before relying on fields. Version 0.157.1's generated schema deprecates full-history hydration for paginated threads and exposes turn pagination with item-detail selection. This project does not currently use those interfaces or replay their history semantics.

Provider conversation APIs do not supply the physical source-line contract used by `show` and `calls`. They cannot replace archive retrieval and file recovery together. Using them for a provider-selected conversation would be a separate capability, not a fallback that silently changes archive results.
