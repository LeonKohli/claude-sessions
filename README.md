# claude-sessions

TUI for browsing, searching, and resuming Claude Code sessions.

Replaces the slow bash script that spawned `rg`/`jq`/`fzf` on every keystroke across 1GB+ of JSONL files. This loads in <1ms from cache.

![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go) ![Bubbletea](https://img.shields.io/badge/Bubbletea-TUI-FF75B5)

## Features

- **Cached index** — Gob cache at `~/.cache/claude-sessions/index.gob`, mtime-based invalidation. Cold scan ~800ms, warm ~1ms.
- **Fuzzy search** — Instant word-split substring matching over summary, first prompt, project path, git branch.
- **Deep content search** — `Tab` toggles grep mode. Parallel workers (`NumCPU` goroutines), 300ms debounce, context snippets around matches.
- **Split-panel layout** — Session list (left) + rich preview (right). Conversation messages, token stats, tools used, files modified.
- **Lazy enrichment** — Full JSONL scan runs on preview, extracting tokens, model, tools, files. Results cached in memory for sorting.
- **Sort** — `s` cycles: modified, created, tokens, messages, duration, size.
- **Filter** — `p` cycles project, `d` cycles date (all/today/week/month).
- **Resume** — `Enter` does `chdir(projectPath)` then `exec("claude", "--resume", uuid)`.
- **Copy UUID** — `y` copies session UUID to clipboard.

## Layout

```
╭─ Search: [__________]  [fuzzy]  S:modified  198/198 ────────────╮
│                            │                                     │
│  just now  21ce05..        │  Session: 21ce0506-6762-...         │
│  ~/Documents/myproject     │  Project: ~/Documents/myproject     │
│  > "Design a TUI app..."  │  Branch:  main                      │
│                            │  Time:    14:14 → 15:32 (1h18m)    │
│  2h ago    65d8b9..        │  Stats:   146 msgs │ 1.2 MB        │
│  ~/work/api                │  Tokens:  in: 12K  out: 8.5K       │
│  "fix auth middleware"     │  Tools:   Read, Edit, Bash          │
│                            │  Files:   5 — auth.go, main.go...  │
│                            │  ─────────────────────              │
│                            │  [usr] 14:14 "Design..."            │
│                            │  [ast] 14:15 "I'll start..."        │
├────────────────────────────┴─────────────────────────────────────┤
│ ↑↓ nav  / search  tab deep  s sort  p project  d date           │
│ ⏎ resume  y copy  q quit                                        │
╰──────────────────────────────────────────────────────────────────╯
```

## Keybindings

| Key | Action |
|-----|--------|
| `↑/k` `↓/j` | Navigate |
| `g` / `G` | Jump to top / bottom |
| `Ctrl+u` / `Ctrl+d` | Half page up / down |
| `/` | Focus search input |
| `Tab` | Toggle fuzzy ↔ deep search |
| `Esc` | Clear search / exit search |
| `s` | Cycle sort mode |
| `p` | Cycle project filter |
| `d` | Cycle date filter |
| `Enter` | Resume selected session in Claude Code |
| `y` | Copy session UUID to clipboard |
| `q` / `Ctrl+c` | Quit |

## How it works

### Session discovery

Claude Code stores sessions under `~/.claude/projects/<encoded-dir>/`:

1. **`sessions-index.json`** — Pre-built metadata (UUID, summary, firstPrompt, messageCount, timestamps, projectPath, gitBranch). Covers ~25% of sessions.
2. **`.jsonl` files** — Raw conversation logs. First 100 lines scanned for first user prompt, CWD, git branch, timestamp.

Filters out:
- Agent files (`agent-*.jsonl`)
- Sidechains
- Ghost entries (index points to non-existent files)
- Empty sessions (no user messages — progress events, file-history-snapshots only)

### Path resolution

Project directory names are encoded (`-Users-leon-Documents` → `/Users/leon/Documents`). Resolution hierarchy:

1. `originalPath` from `sessions-index.json`
2. `projectPath` from session index entry
3. `cwd` field from first JSONL message
4. Decoded directory name (last resort)

### Caching

Gob-encoded `[]SessionEntry` + mtime map. Invalidated when any project directory or `sessions-index.json` is modified. Cache saved in background goroutine after scan.

### Enrichment

On first preview of a session, full JSONL scan extracts:
- Token usage (input, output, cache read, cache write)
- Primary model (most frequent across assistant messages)
- Tools used (from `tool_use` content blocks)
- Files modified (from `file-history-snapshot` entries)
- Actual message count

Pre-filters lines with `strings.Contains` before JSON parsing for performance.

## Architecture

```
main.go                          # Load → TUI → ExecResume
internal/
  session/
    types.go                     # SessionEntry, Message, Usage structs
    reader.go                    # JSONL streaming (bufio.Scanner, pre-filter)
    enrich.go                    # Full scan: tokens, model, tools, files
  index/
    scanner.go                   # Walk projects dir, parse index + JSONL
    cache.go                     # Gob cache with mtime invalidation
    index.go                     # Load orchestrator (cache → scan → save)
  tui/
    app.go                       # Bubbletea model, Update loop, filters, sort
    view.go                      # Render: search bar, list, preview, status
    deep.go                      # Parallel grep across JSONL files
    keymap.go                    # Key bindings
    styles.go                    # Lipgloss theme (purple/gray)
    messages.go                  # Tea message types
  util/
    path.go                      # Path resolution, formatting (time, tokens, size)
    clipboard.go                 # pbcopy wrapper
```

## Build & install

```bash
go build -ldflags="-s -w" -o claude-sessions .
# Atomic install (avoids macOS code signature issues):
mv claude-sessions ~/.local/bin/
codesign -s - ~/.local/bin/claude-sessions
```

## Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| [bubbletea](https://github.com/charmbracelet/bubbletea) | v1.3 | TUI framework |
| [bubbles](https://github.com/charmbracelet/bubbles) | v0.21 | Text input component |
| [lipgloss](https://github.com/charmbracelet/lipgloss) | v1.1 | Terminal styling |

No runtime dependencies. No `rg`, `jq`, `fzf`, or any external tools needed.
