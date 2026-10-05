# Development

The project builds with Go 1.26 or later on macOS and Linux. SQLite comes from `modernc.org/sqlite`, a pure Go build, so no C compiler or system library is needed.

## Build from the checkout

```bash
go build -ldflags="-s -w" -o agent-sessions ./cmd/agent-sessions
mv agent-sessions ~/.local/bin/
```

## Run the tests

```bash
go test ./...
go vet ./...
```

To keep tests independent of your own history, point the provider roots and the cache at empty temporary directories. CI does the same:

```bash
export CLAUDE_CONFIG_DIR="$(mktemp -d)" CODEX_HOME="$(mktemp -d)" CODEX_SQLITE_HOME="$(mktemp -d)" XDG_CACHE_HOME="$(mktemp -d)"
go test ./...
```

`TestCommandsWithRelocatedStores` runs the CLI dispatcher against synthetic Claude and Codex stores, including a project path that contains spaces. `AGENTS.md` describes how tests in this repository are written.

## Release a version

CI runs the tests on Linux and macOS for every push to `main`. A pushed `v*` tag runs the tests again and then [GoReleaser](https://goreleaser.com/), which publishes four archives (macOS and Linux, amd64 and arm64) and `SHA256SUMS`. Each archive contains the binary, `README.md`, the `docs/` pages, and the `skills/search-sessions/` skill from the same commit.

To check the packaging locally without publishing, run the GoReleaser version that CI uses:

```bash
go run github.com/goreleaser/goreleaser/v2@v2.18.2 check
go run github.com/goreleaser/goreleaser/v2@v2.18.2 release --snapshot --clean
```

To publish, tag the commit and push the tag:

```bash
git tag -a v0.3.0 -m "v0.3.0"
git push origin main v0.3.0
```

## Code layout

```
cmd/agent-sessions/main.go   # runs a subcommand, or the browser on a terminal
internal/
  cli/                       # non-interactive commands
    dispatch.go              # routing, flags, error codes, schema
    commands.go, calls.go    # list, search, show, calls, files, cat, diff, resume
    output.go, text.go       # envelope, terminal detection, text output
    args.go                  # flag aliases, flags after operands
  provider/provider.go       # provider kinds, store locations, resume commands
  session/
    types.go                 # session entries and shared readers
    reader.go, enrich.go     # Claude transcripts
    codex.go, history.go     # Codex rollouts and their ancestors
    calls.go                 # recorded tool inputs
    files.go                 # file changes and recovery
    lines.go                 # line reader for long transcript lines
    jsonscan.go              # locates JSON values without decoding them
  index/
    index.go, cache.go       # discovery and the saved session listing
    scanner.go               # Claude store scan
    codex.go                 # Codex thread database and rollout scan
    database.go              # SQLite schema, transcript text, full-text index
    search.go                # search, shared by the CLI and the browser
    workload.go              # cross-process lock
  tui/                       # browser
  util/                      # paths, formatting, clipboard
```

## Dependencies

| Module | Used for |
|---|---|
| [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Bubbles](https://github.com/charmbracelet/bubbles), [Lip Gloss](https://github.com/charmbracelet/lipgloss), [Glamour](https://github.com/charmbracelet/glamour) | Browser |
| [modernc.org/sqlite](https://modernc.org/sqlite) | Search index and the Codex thread database |
| [golang.org/x/term](https://pkg.go.dev/golang.org/x/term) | Terminal detection |
| [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys) | Index lock |
