# Browser reference

`agent-sessions` without arguments opens the browser when stdin and stdout are a terminal. `--claude` or `--codex` limits it to one provider. The browser needs a terminal of at least 40 columns and 18 rows.

## Keys in the session list

| Key | Action |
|---|---|
| `↑` `k`, `↓` `j` | Select the previous or next session |
| `g`, `G` | Jump to the first or last session |
| `Ctrl+u`, `Ctrl+d` | Page up or down |
| `/` | Search sessions. `Ctrl+s` switches between titles and metadata, and full transcripts. `↑` `↓` select while typing. |
| `Tab` | Move focus between the list and the preview |
| `[`, `]` | Scroll the preview |
| `s` | Choose the sort order: time, usage, or size |
| `p` | Choose a project |
| `d` | Choose a date range |
| `f` | Choose an agent: all, Claude, or Codex |
| `a` | Show or hide subagent sessions |
| `Enter` | Open the conversation reader |
| `o` | Open the file browser |
| `r` | Quit and resume the session with its agent, in the session's project directory |
| `y` | Copy the session ID |
| `Ctrl+k`, `F1`, `?` | Open the action menu. `?` works only outside a search field. |
| `q` | Quit |

Navigation keys act on the focused pane. With the preview focused, `g` and `G` scroll the preview and keep the selected session.

## Action menu

The action menu lists every action of the current view. Type to filter it, select with `↑` `↓`, apply with `Enter`, and close with `Esc`. Closing the menu keeps the previous query, selection, and focus. The menu is available while you edit a query, in the reader, and in the file browser.

## Conversation reader

| Key | Action |
|---|---|
| `/` | Find text in the conversation |
| `n`, `N` | Jump to the next or previous match |
| `m` | Switch between rendered Markdown and source text |
| `Esc` | Return to the session list |

The reader renders assistant answers as Markdown and shows user prompts literally. Source mode keeps the original code indentation. Find searches the displayed text.

The list preview shows the first 30 messages. The reader loads more and stops after the message that crosses 8 MiB of conversation text. To read past that point, use `agent-sessions show`. The reader and the preview show conversation text only, without tool activity or reasoning.

## File browser

| Key | Action |
|---|---|
| `Tab` | Move focus between the file list and the content |
| `↑` `↓` | Select a file |
| `PgUp`, `PgDn` | Scroll the content |
| `Esc` | Return to the session list |

The file browser is read-only. It marks whether an entry holds file content or a recorded diff, and where the content came from. The preview shows the first 64 KiB of a file. To get the complete record, use `agent-sessions cat` or `agent-sessions diff`. [Recover a file](recover-a-file.md) explains what a recorded version proves.

## Layout

Each session takes two lines: a Claude or Codex badge, the title, and the project and date. Below 90 columns the list sits above the preview instead of beside it. Colors adapt to light and dark terminals. Names and selection markers stay readable without color.

## Clipboard

`y` copies with `pbcopy` on macOS, `wl-copy` on Wayland, and `xclip` on X11. Without a clipboard command, the browser shows an error.
