# How to recover a file from an earlier session

Use this guide when you need a file as it was during a past Claude Code or Codex session. You need the session ID. To find it, run `agent-sessions search "<distinctive term>" --json`.

1. List the files the session changed:

	```bash
	agent-sessions files <id> --json
	```

	Each entry has `kind` (`add`, `update`, or `delete`), `revisions`, and `recoverable`. Claude entries also have `recovery_source` and `recovery_earlier_version`.

2. If `recoverable` is `true`, write the content to a new file:

	```bash
	agent-sessions cat <id> lib/links.ts > /tmp/links.ts
	```

	`cat` writes raw bytes without an envelope. A bare file name works when it matches one path in the session.

3. If `recoverable` is `false` for a Codex `update`, print the recorded diffs instead:

	```bash
	agent-sessions diff <id> lib/links.ts
	```

	Codex stores updates as unified diffs only. `cat` returns `not_recoverable` for them.

4. Before you restore the file, check what the bytes are:

	- If `recovery_source` is `claude_checkpoint`, the bytes come from a Claude checkpoint file.
	- If `recovery_source` is `claude_write`, the bytes come from a successful `Write` call in the transcript.
	- If `recovery_earlier_version` is `true`, a newer checkpoint is missing, and `cat` returned an earlier version.

	A recovered file is the latest version the archive recorded. It is not proof of the file's state when the session ended.

## What the archive cannot give back

The archive holds only the bytes an agent recorded. These changes leave no recoverable content:

- Shell commands, Python scripts, and MCP tools that wrote the file. Claude checkpoints do not cover shell edits.
- Claude `Edit` calls. Their checkpoints still work, but the tool does not rebuild a file from `Edit` results.
- Claude checkpoints that expired with session retention. See [Claude checkpointing](https://code.claude.com/docs/en/checkpointing).
- Codex patches that failed or were declined.

A file missing from `files` can still have changed during the session. `agent-sessions` never runs recorded commands to rebuild a file. State which content you could not recover instead.

## How each provider records files

| | Claude Code | Codex |
|---|---|---|
| Records | Checkpoint files and successful `Write` results | Patch events in the rollout |
| Added and deleted files | Checkpoint bytes, when readable | Recorded content, when present |
| Updated files | The latest recorded content | A unified diff only |

Claude recovery picks the latest readable checkpoint or confirmed `Write` result in transcript order. A `Write` result counts only when it matches its tool call and reports success. Codex recovery uses only successful patch records. An empty file recorded by Codex is recoverable as zero bytes.
