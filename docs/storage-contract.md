# Storage and evidence

`agent-sessions` reads local archives. Its output does not reconstruct the exact context an agent receives on resume, and it is not a complete record of filesystem changes.

This reference separates what the providers document from what the reader observed in their internal formats. The observations cover Claude Code 2.1.72 and 2.1.266, and Codex 0.157.1 and 0.158.0-alpha.2. These versions are examples of compatibility, not a supported range.

## Stores

| Store | Contents | Reader |
|---|---|---|
| Claude `projects/<project>/<session>.jsonl` under `CLAUDE_CONFIG_DIR`, default `~/.claude` | Messages, tool records, and session metadata | `internal/session/reader.go`, `calls.go` |
| Claude `<session>/subagents/agent-*.jsonl` | Separate subagent transcripts | `internal/index/scanner.go` |
| Claude checkpoint files referenced by `file-history-snapshot` records | Historical file bytes, subject to retention | `internal/session/files.go` |
| Codex `sessions/` and `archived_sessions/` under `CODEX_HOME`, default `~/.codex` | Rollout records | `internal/session/codex.go`, `calls.go` |
| Codex `state_<n>.sqlite` under `CODEX_SQLITE_HOME` or `CODEX_HOME` | Thread metadata and rollout locations | `internal/index/codex.go` |
| `agent-sessions/index.sqlite` in the user cache directory | Rebuildable metadata and conversation text | `internal/index/database.go` |

Claude documents its JSONL format as internal and subject to change between releases. Its project directory names are not reversible: the encoding replaces non-alphanumeric characters, and it can truncate long names or use a configured name. The reader prefers recorded working directories over decoded directory names. Retention settings and disabled persistence can remove local history or prevent it. See [Claude session storage](https://code.claude.com/docs/en/sessions).

The Codex database supplies discovery metadata and selects the current rollout of each thread. When the database is absent, unreadable, or empty, discovery scans the rollout files instead. A rollout that the database selects but that is missing is an error, so the scan never replaces it with older history. Discovery does not add unlisted files to a nonempty database result. The tool does not read Codex's `sqlite_home` setting. When it finds no database at the selected location, it reads the rollout files.

## Codex record types

Each rollout line has the shape `{timestamp, type, payload}`. The reader uses these records:

| Record | Contents |
|---|---|
| `session_meta`, first line | Thread ID, working directory, git branch, CLI version, `thread_source` |
| `turn_context` | Model, reasoning effort, sandbox policy |
| `response_item` with payload `message` | User `input_text` and assistant `output_text`. Preferred over the legacy events below. |
| `event_msg` with payload `user_message` or `agent_message` | The conversation in older rollouts |
| `event_msg` with payload `token_count` | Running totals. The last record counts. Records are not summed. |
| `event_msg` with a successful `patch_apply_end` | `changes`: content for adds and deletes, `unified_diff` for updates |
| `event_msg` with `item_completed` and a completed `FileChange` | The same change data in newer rollouts |
| `response_item` with payload `function_call` or `custom_tool_call` | Tool names and inputs |

## Raw records differ from resumed history

Claude records carry message UUIDs, parent links, compaction boundaries, and generated summaries. A generated summary can have the role `user`, so the role alone does not prove that a person wrote it. The SDK's store-backed `getSessionMessages` returns the linked chain after compaction. A store adapter's `load` returns raw entries, including earlier history, and the application implements that adapter. The store does not copy checkpoint files. See [Claude session-store semantics](https://code.claude.com/docs/en/agent-sdk/session-storage).

The Claude reader in `agent-sessions` returns text in file order. It does not follow parent links or compaction flags. This keeps all archived evidence but does not identify the branch the agent continued. Both official SDKs keep the role `user` on summaries. The SDKs also differ from each other: TypeScript 0.3.283 restores preserved compaction segments and sibling response fragments that Python 0.2.160 omits. Matching a simple parent walk would therefore still not reproduce the resumed context.

Codex separates model-input `response_item` records from conversation events. In observed rollouts, user-role response items include injected `AGENTS.md` instructions. The reader can return those instructions as the first user turn and use them as a fallback title. Upstream's [history projection](https://github.com/openai/codex/blob/1b1835f751ebdc0cfc50b3fe55d4571dbb294563/codex-rs/app-server-protocol/src/protocol/thread_history.rs) treats these record types differently.

Codex archive IDs have the form `<thread-id>/<rollout-id>` for recognized rollout file names. The original rollout repeats the thread ID as its rollout ID. These IDs keep search, retrieval, and browser previews on the same physical history. A logical ID alone resolves only when it is unambiguous. Resume commands always use the logical ID.

Codex readers follow `history_base.thread_id` to an ancestor rollout and read the ancestor's records before the local ones. `end_byte_offset` and `end_ordinal_exclusive` bound each ancestor. Conversation, calls, file evidence, and metrics share this traversal. `show` and `calls` return the physical `source` path and line of each record. Missing or ambiguous ancestors, cycles, invalid cutoffs, and compressed `.zst` histories are errors. See the pinned upstream [lineage rules](https://github.com/openai/codex/blob/1b1835f751ebdc0cfc50b3fe55d4571dbb294563/codex-rs/thread-store/src/local/rollout_lineage.rs) and [rollout selection](https://github.com/openai/codex/blob/1b1835f751ebdc0cfc50b3fe55d4571dbb294563/codex-rs/thread-store/src/local/thread_rollout_resolver.rs).

The result is the referenced archive ranges, not the provider's full conversation projection. Injected instructions stay visible, and the reader does not replay legacy rollback markers. Claude branch selection is not supported. Treat conversation history and filesystem state separately: a rewind of the conversation alone can keep the file writes of the discarded branch.

## Attempts, results, and file bytes

`calls` returns attempted tool inputs. Only the matching tool result shows that a call succeeded. An assistant's statement or the tool name does not. File recovery accepts recognized successful writes, successful patch events, and readable checkpoints. It never runs recorded commands.

Claude stores checkpoints apart from the transcript, and they can expire. The current documentation limits how many checkpoints Claude keeps and excludes Bash edits from them. A copied transcript can therefore lack recoverable file bytes. See [checkpointing](https://code.claude.com/docs/en/checkpointing). `recovery_source` names the recorded source of the bytes, and `recovery_earlier_version` marks a fallback to an older version. Neither proves the final state of the file. Codex updates hold diffs, not complete files. [Recover a file](recover-a-file.md) covers the steps.

Claude can repeat usage across content fragments that share one `message.id`. Browser metrics count each response once with its latest usage, and collect tools from all fragments. Records without a response ID are counted individually. These totals are what the provider reported, not measurements of retrieval savings. The CLI does not use these metrics.

## Empty and partial results

`truncated` reports omitted records. Search snippets are previews whether or not `truncated` is present. [The CLI reference](cli.md) lists the fields.

An empty search means no recognized match among the discovered sessions under the filters. Missing provider directories are allowed. An error while listing a selected provider's directories returns `store_unavailable`, not an empty success. Discovery can still skip individual transcripts with unreadable or unrecognized headers, and parsing skips malformed records. Search compares file signatures, uses stored text for unchanged transcripts, and returns errors from refreshing changed ones. Missing or ambiguous Codex ancestors fail even after indexing. An empty result therefore does not prove complete coverage of history.

## Provider interfaces

The index package owns discovery and cached metadata. The session package owns archive traversal, record interpretation, and recovery evidence. The CLI and the browser read conversations through `WalkSearchable`. `ReadPreview` collects only a bounded prefix. JSON decoding decides record types. A byte-level filter can skip irrelevant records, but it must let records with escaped strings through to decoding.

Claude's [Python SDK](https://code.claude.com/docs/en/agent-sdk/python) offers `list_sessions` and `get_session_messages` for saved local sessions. For history as the provider interprets it, these functions are a better comparison than another independent JSONL parser. Resuming with a new prompt is a different operation and can create new records.

Codex documents read-only history retrieval through `thread/read` and the paginated `thread/turns/list`. It also documents rollback markers and moves to the archive. See the [app-server protocol](https://developers.openai.com/codex/app-server). Generate the installed version's schema with `codex app-server generate-json-schema --out <temporary-directory>` before you rely on its fields. The schema of version 0.157.1 deprecates loading the full history of paginated threads, and it adds turn pagination with a selection of item details. `agent-sessions` does not use these interfaces and does not replay their history rules.

Provider conversation APIs do not return the physical source lines that `show` and `calls` report. They cannot replace archive retrieval and file recovery together. Using them for the conversation a provider selects would be a separate feature, not a fallback that silently changes archive results.
