# CLI reference

`agent-sessions <command> [operands] [flags]` runs one command and exits. `agent-sessions schema --json` returns the same contract as JSON, including the payload fields of each command.

## Commands

| Command | Returns | Default `--limit` |
|---|---|---|
| `list` | Sessions, newest first | 20 |
| `search <query>` | Sessions whose conversation contains the query, ranked by match count, with snippets | 10 |
| `show <id> [query]` | Conversation turns in transcript order, optionally only the turns that contain the query | 30 |
| `calls <id> [query]` | Recorded tool inputs, optionally only the inputs that contain the query | 10 |
| `files <id>` | Files the session changed, and whether their content is recoverable | none |
| `cat <id> <path>` | One recovered file as raw bytes, without an envelope | none |
| `diff <id> <path>` | The unified diffs Codex recorded for a file | none |
| `resume <id>` | The command that reopens the session, the working directory, and `cwd_exists` | none |
| `schema` | Commands, flags, payload fields, limits, and error codes | none |

`resume` prints the command. It never runs it.

## Flags

| Flag | Values | Default | Effect |
|---|---|---|---|
| `--output` | `auto`, `json`, `text` | `auto` | `auto` writes text to a terminal and JSON to a pipe |
| `--agent` | `claude`, `codex` | both | Selects one provider |
| `--project` | substring | none | Keeps sessions whose project path contains the substring |
| `--since` | Go duration, such as `72h` | none | Keeps sessions modified within the duration. Days (`30d`) are not accepted. |
| `--limit` | integer | per command | Bounds returned records. `0` selects the default. |
| `--snippets` | integer | 3 | Snippets per search hit. `0` omits snippets and keeps match counts. |
| `--max-chars` | integer | 500 | Bounds each snippet, turn, or input in Unicode characters. `0` returns complete turns and inputs. Search snippets stay previews. |
| `--subagents` | boolean | false | Includes subagent sessions |

`--claude` and `--codex` select a provider and override `--agent`. Both together select both providers.

Flags can come before or after operands. Operands can also be passed by name: `--id`, `--query`, and `--path`. `show --query error --id <id>` is the same as `show <id> error`.

## Accepted spellings

The CLI maps common spellings from other tools to its own flags. An unknown flag returns `usage_error` with the list of accepted flags.

| Spelling | Maps to |
|---|---|
| `--json`, `--robot` | `--output json` |
| `--format` | `--output` |
| `--max_results`, `--max-results`, `--num`, `-n` | `--limit` |
| `--max_chars`, `--maxchars` | `--max-chars` |
| `--provider`, `--tool` | `--agent` |
| `--cwd`, `--workspace` | `--project` |
| `-q`, `--text`, `--pattern` | `--query` |
| `--session` | `--id` |
| `--file` | `--path` |

A multi-word query works without quotes: `search unified diff --limit 2` searches for `unified diff`.

## Session IDs

Pass the `id` from `list` or `search` unchanged. Codex archive IDs have the form `<thread-id>/<rollout-id>`, because one Codex thread can have several physical rollouts. A logical thread ID alone works when it selects one rollout. `resume` always prints the logical thread ID.

Any unambiguous ID prefix works. An ambiguous prefix returns `ambiguous_id`. Text output shows 12-character prefixes.

## Queries

Queries are literal substrings, compared after Unicode lowercasing. They are not word searches and not semantic searches. Punctuation is kept.

`search` matches conversation text. `show` matches complete turns and filters before it applies `--limit`. `calls` matches tool names, call IDs, and complete inputs, and also filters before it applies `--limit`.

## Output envelope

JSON output has this envelope:

```json
{"ok":true,"schema":1,"cmd":"search","data":{"query":"error","hits":[…],"count":5},
 "truncated":{"returned":5,"total":848,"has_more":true,"hint":"raise --limit to see more (total 848)"}}
```

| Field | Meaning |
|---|---|
| `data` | The command's payload. `schema --json` lists its fields under `data.response_fields`. |
| `truncated` | Present when records were omitted. `total` is present only when the command counted all records. |
| `text_truncated` | On a `show` turn: the text is an excerpt around the first match. |
| `input_truncated` | On a `calls` record: the input is an excerpt around the match. |

A missing payload field is a contract mismatch, not an empty result. `cat` writes raw bytes even with `--json`.

`show` and `calls` records carry the transcript `line`. Codex records also carry `source`, the physical rollout path.

## Errors

A failure exits with a nonzero status and writes the envelope to stderr. Stdout stays empty.

```json
{"ok":false,"schema":1,"cmd":"cat","error":{
  "code":"not_recoverable",
  "message":"codex recorded links.ts as \"update\", which stores only a unified diff…",
  "hint":"try `agent-sessions diff <id> <path>`"}}
```

| Code | Cause |
|---|---|
| `usage_error` | Unknown command or flag, or a missing operand |
| `not_found` | No session with that ID, or no recorded file at that path |
| `ambiguous_id` | The ID prefix matches more than one session |
| `store_unavailable` | A provider store, a referenced Codex history, or the index lock is unavailable. The index lock waits at most five seconds. |
| `not_recoverable` | The archive holds no complete content for the file |
| `internal_error` | Any other failure |

## Search behavior

`search` fails when a selected transcript is missing or cannot be read. It skips malformed JSON lines, so the unfinished last line of an active session does not stop it.

An empty result means no match among the discovered sessions under the current filters. It does not prove that a conversation never happened. [Storage and evidence](storage-contract.md) lists what the archives can miss.

## Examples

Read the complete turns that mention a file:

```bash
agent-sessions show <id> experiments.md --json --limit 20 --max-chars 0
```

Find a shell command and keep the truncation warning while projecting fields:

```bash
agent-sessions calls <id> pcap-dir --json --limit 5 |
	jq '{truncated, calls: [.data.calls[] | {id,tool,source,line,input,input_truncated}]}'
```

Decode the command of a complete Claude `Bash` input:

```bash
agent-sessions calls <id> <call-id> --json --max-chars 0 | jq -r '.data.calls[].input | fromjson | .command'
```

`jq` sees only returned records. Raise `--limit` instead of filtering more records out of a truncated result.
