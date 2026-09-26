#!/usr/bin/env bash
set -euo pipefail

scope=()
query=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --claude) scope=(--agent claude); shift ;;
    --codex) scope=(--agent codex); shift ;;
    --) shift; query+=("$@"); break ;;
    --help|-h) printf '%s\n' 'Usage: search-sessions [--claude|--codex] [query]'; exit 0 ;;
    *) query+=("$1"); shift ;;
  esac
done

if [[ ${#query[@]} -gt 0 ]]; then
  exec agent-sessions search "${scope[@]}" --json -- "${query[*]}"
fi
case "${scope[1]:-}" in
  claude) exec agent-sessions --claude ;;
  codex) exec agent-sessions --codex ;;
  *) exec agent-sessions ;;
esac
