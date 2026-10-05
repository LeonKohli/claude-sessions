# agent-sessions

Search, read, and recover files from Claude Code and Codex session history.

Both agents store their sessions as JSONL files under `~/.claude` and `~/.codex`, and their own pickers show only the sessions of the current directory. `agent-sessions` indexes both stores and gives you two interfaces: an interactive browser for you, and a non-interactive CLI with bounded JSON output for a coding agent.

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go) ![Bubbletea](https://img.shields.io/badge/Bubbletea-TUI-FF75B5)

## Install

Download an archive for macOS or Linux (amd64 or arm64) from the [releases page](https://github.com/LeonKohli/claude-sessions/releases). Check it against the release's `SHA256SUMS`, extract it, and move `agent-sessions` to a directory on your `PATH`.

To install from source instead, use a published tag or an exact commit:

```bash
go install github.com/LeonKohli/claude-sessions/cmd/agent-sessions@<version>
```

The binary has no runtime dependencies. On Linux, the browser's copy action needs `wl-copy` or `xclip`.

## Get started

To browse your sessions, run the command without arguments in a terminal:

```bash
agent-sessions
```

To query sessions from a script or an agent, pass a subcommand:

```bash
agent-sessions search "webhook retry" --json --limit 5
agent-sessions show <id> --json --limit 20
agent-sessions files <id> --json
```

With a subcommand, or with output piped, the tool never opens the browser and never waits for input. The first search builds a full-text index, which takes about 30 seconds for 20 GB of transcripts. Later searches refresh only changed files.

To let Claude Code or Codex use the tool, copy or link `skills/search-sessions/` into the agent's skills directory. The archive contains this skill. The skill calls the binary on your `PATH`.

## Documentation

| Document | Read it to |
|---|---|
| [CLI reference](docs/cli.md) | look up commands, flags, output fields, and error codes |
| [Browser reference](docs/browser.md) | look up key bindings and browser views |
| [Recover a file](docs/recover-a-file.md) | get a file back as it was in an earlier session |
| [How it works](docs/how-it-works.md) | understand discovery, the search index, and the design choices |
| [Storage and evidence](docs/storage-contract.md) | know what the archives can and cannot prove |
| [Development](docs/development.md) | build, test, and release the project |
