package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/LeonKohli/claude-sessions/internal/index"
	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

// Bounded-output defaults. An agent pays for every token it reads back, so the
// defaults are deliberately small and callers widen them explicitly.
const (
	DefaultListLimit    = 20
	DefaultSearchLimit  = 10
	DefaultShowMessages = 30
	DefaultSnippets     = 3
	DefaultMaxChars     = 500
)

// SessionRef is the compact session shape shared by list and search.
type SessionRef struct {
	ID        string `json:"id"`
	Agent     string `json:"agent"`
	Title     string `json:"title"`
	Project   string `json:"project"`
	Branch    string `json:"branch,omitempty"`
	Model     string `json:"model,omitempty"`
	Modified  string `json:"modified"`
	Messages  int    `json:"messages,omitempty"`
	Subagent  bool   `json:"subagent,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
	Parent    string `json:"parent,omitempty"`
	Resume    string `json:"resume"`
}

func toRef(e session.SessionEntry) SessionRef {
	return SessionRef{
		ID:        e.SessionID,
		Agent:     e.Provider.String(),
		Title:     e.DisplayTitle(),
		Project:   e.ProjectPath,
		Branch:    e.GitBranch,
		Model:     e.Model,
		Modified:  e.Modified.UTC().Format(time.RFC3339),
		Messages:  e.MessageCount,
		Subagent:  e.IsSubagent,
		AgentName: e.AgentLabel,
		Parent:    e.Parent,
		Resume:    resumeCommand(e),
	}
}

// Filter narrows which sessions a command considers.
type Filter struct {
	Provider  string // "", "claude", "codex"
	Project   string
	Since     time.Duration
	Subagents bool
	Limit     int
}

func (f Filter) kinds() []provider.Kind {
	switch f.Provider {
	case "claude":
		return []provider.Kind{provider.Claude}
	case "codex":
		return []provider.Kind{provider.Codex}
	default:
		return provider.All
	}
}

func (f Filter) apply(all []session.SessionEntry) []session.SessionEntry {
	out := make([]session.SessionEntry, 0, len(all))
	cutoff := time.Time{}
	if f.Since > 0 {
		cutoff = time.Now().Add(-f.Since)
	}
	for _, e := range all {
		if e.IsSubagent && !f.Subagents {
			continue
		}
		if f.Project != "" && !strings.Contains(e.ProjectPath, f.Project) {
			continue
		}
		if !cutoff.IsZero() && e.Modified.Before(cutoff) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out
}

func load(f Filter) ([]session.SessionEntry, error) {
	all, err := index.Load(f.kinds())
	if err != nil {
		return nil, err
	}
	return f.apply(all), nil
}

// bound truncates a fully-materialised slice, so the total is known.
func bound[T any](items []T, limit int) ([]T, *Truncation) {
	if limit <= 0 || len(items) <= limit {
		return items, nil
	}
	return items[:limit], &Truncation{
		Returned: limit,
		Total:    len(items),
		HasMore:  true,
		Hint:     fmt.Sprintf("raise --limit to see more (total %d)", len(items)),
	}
}

// boundLazy truncates a slice read one item past the limit purely to detect
// whether more exist. The true size was never counted, so it is left unset.
func boundLazy[T any](items []T, limit int) ([]T, *Truncation) {
	if limit <= 0 || len(items) <= limit {
		return items, nil
	}
	return items[:limit], &Truncation{
		Returned: limit,
		HasMore:  true,
		Hint:     "more turns exist; raise --limit (total not counted)",
	}
}

// ─────────────────────────────────────────── list

// List enumerates sessions, newest first.
func List(f Filter) (Result, error) {
	sessions, err := load(f)
	if err != nil {
		return Result{}, err
	}

	refs := make([]SessionRef, 0, len(sessions))
	for _, e := range sessions {
		refs = append(refs, toRef(e))
	}
	shown, trunc := bound(refs, f.Limit)

	return Result{
		Data:      map[string]any{"sessions": shown, "count": len(shown)},
		Truncated: trunc,
		Text: func(w io.Writer) {
			p := printer{w}
			for _, r := range shown {
				p.line("%s  %-6s  %s", shortID(r.ID), r.Agent, truncate(r.Title, 70))
				p.line("          %s", r.Project)
			}
		},
	}, nil
}

// ─────────────────────────────────────────── search

// Hit is one session matching a search, with the lines that matched.
type Hit struct {
	SessionRef
	Matches  int      `json:"matches"`
	Snippets []string `json:"snippets"`
}

// Search finds sessions whose transcript contains query.
//
// It returns ranked sessions with short snippets rather than raw transcript.
// That is the point of the command: an agent needs just enough to choose a
// session, then fetches that one — dumping matching lines from a multi-GB
// store costs more context than the answer is worth.
func Search(query string, f Filter, snippets, maxChars int) (Result, error) {
	if strings.TrimSpace(query) == "" {
		return Result{}, fmt.Errorf("empty query")
	}
	sessions, err := load(f)
	if err != nil {
		return Result{}, err
	}

	hits, err := index.SearchSessions(context.Background(), sessions, query, snippets, maxChars)
	if err != nil {
		return Result{}, err
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Matches != hits[j].Matches {
			return hits[i].Matches > hits[j].Matches
		}
		return hits[i].Session.Modified.After(hits[j].Session.Modified)
	})

	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		out = append(out, Hit{SessionRef: toRef(h.Session), Matches: h.Matches, Snippets: h.Snippets})
	}
	shown, trunc := bound(out, f.Limit)

	return Result{
		Data:      map[string]any{"query": query, "hits": shown, "count": len(shown)},
		Truncated: trunc,
		Text: func(w io.Writer) {
			p := printer{w}
			for _, h := range shown {
				p.line("%s  %-6s  %d matches  %s", shortID(h.ID), h.Agent, h.Matches, truncate(h.Title, 50))
				for _, s := range h.Snippets {
					p.line("    %s", s)
				}
			}
		},
	}, nil
}

// ─────────────────────────────────────────── show

// Turn is one conversation message.
type Turn struct {
	Role string `json:"role"`
	At   string `json:"at"`
	Text string `json:"text"`
}

// Show returns a session's conversation.
func Show(id string, limit, maxChars int) (Result, error) {
	e, err := Resolve(id)
	if err != nil {
		return Result{}, err
	}

	msgs, err := session.ReadPreview(e.Provider, e.FullPath, limit+1)
	if err != nil {
		return Result{}, err
	}

	turns := make([]Turn, 0, len(msgs))
	for _, m := range msgs {
		turns = append(turns, Turn{
			Role: m.Role,
			At:   m.Timestamp.UTC().Format(time.RFC3339),
			Text: truncate(m.Text, maxChars),
		})
	}
	shown, trunc := boundLazy(turns, limit)

	return Result{
		Data:      map[string]any{"session": toRef(e), "turns": shown},
		Truncated: trunc,
		Text: func(w io.Writer) {
			p := printer{w}
			for _, t := range shown {
				p.line("[%s] %s", t.Role, t.At)
				p.line("%s", t.Text)
				p.blank()
			}
		},
	}, nil
}

// ─────────────────────────────────────────── files

// Files lists what a session changed on disk.
func Files(id string) (Result, error) {
	e, err := Resolve(id)
	if err != nil {
		return Result{}, err
	}
	changes, err := session.FileChanges(e)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Data: map[string]any{"session": toRef(e), "files": changes, "count": len(changes)},
		Text: func(w io.Writer) {
			p := printer{w}
			for _, c := range changes {
				mark := " "
				if c.Recoverable {
					mark = "*"
				}
				p.line("%s %-7s rev%-3d %s", mark, c.Kind, c.Revisions, c.Path)
			}
			if len(changes) > 0 {
				p.blank()
				p.line("* = content recoverable with `agent-sessions cat`")
			}
		},
	}, nil
}

// ─────────────────────────────────────────── cat / diff

// Cat writes one file's stored content verbatim, with no envelope: the caller
// asked for file bytes and should be able to redirect them straight to disk.
func Cat(id, path string) (Result, error) {
	e, err := Resolve(id)
	if err != nil {
		return Result{}, err
	}
	content, err := session.RecoverContent(e, path)
	if err != nil {
		return Result{}, err
	}
	return Result{Raw: content}, nil
}

// Diff writes the unified diffs Codex recorded for a file.
func Diff(id, path string) (Result, error) {
	e, err := Resolve(id)
	if err != nil {
		return Result{}, err
	}
	diffs, err := session.CodexDiff(e, path)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Data: map[string]any{"session": toRef(e), "path": path, "diffs": diffs},
		Text: func(w io.Writer) {
			p := printer{w}
			for _, d := range diffs {
				p.raw(d)
				p.blank()
			}
		},
	}, nil
}

// ─────────────────────────────────────────── resume

// Resume reports how to reopen a session. It prints rather than execs, because
// an agent cannot hand its terminal to an interactive process.
func Resume(id string) (Result, error) {
	e, err := Resolve(id)
	if err != nil {
		return Result{}, err
	}
	if e.Provider == provider.Claude && e.IsSubagent {
		return Result{}, fmt.Errorf("claude subagent transcripts cannot be resumed")
	}
	cmd := resumeCommand(e)

	// Sessions recorded on another machine, or in a directory since moved,
	// still resolve here. Saying so beats handing back a `cd` that fails.
	cwdExists := false
	if e.ProjectPath != "" {
		if info, err := os.Stat(e.ProjectPath); err == nil && info.IsDir() {
			cwdExists = true
		}
	}

	return Result{
		Data: map[string]any{
			"session":    toRef(e),
			"cwd":        e.ProjectPath,
			"cwd_exists": cwdExists,
			"command":    cmd,
		},
		Text: func(w io.Writer) {
			p := printer{w}
			p.line("cd %s && %s", shellQuote(e.ProjectPath), cmd)
			if !cwdExists {
				p.line("warning: %s does not exist on this machine", e.ProjectPath)
			}
		},
	}, nil
}

func resumeCommand(e session.SessionEntry) string {
	_, argv := e.Provider.ResumeArgv(e.SessionID)
	argv[len(argv)-1] = shellQuote(e.SessionID)
	return strings.Join(argv, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// ─────────────────────────────────────────── shared

// Resolve finds a session by full or abbreviated id. Every listing shows an
// 8-character prefix, so that is what callers will pass back.
func Resolve(id string) (session.SessionEntry, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return session.SessionEntry{}, fmt.Errorf("no session id given")
	}
	all, err := index.Load(provider.All)
	if err != nil {
		return session.SessionEntry{}, err
	}

	var hits []session.SessionEntry
	for _, e := range all {
		if e.SessionID == id {
			return e, nil // an exact id wins outright
		}
		if strings.HasPrefix(e.SessionID, id) {
			hits = append(hits, e)
		}
	}
	switch len(hits) {
	case 0:
		return session.SessionEntry{}, fmt.Errorf("no session matches %q", id)
	case 1:
		return hits[0], nil
	default:
		return session.SessionEntry{}, fmt.Errorf("%q matches %d sessions, use a longer id", id, len(hits))
	}
}

// shortIDLen matches the index: 8 characters collide for most sessions because
// Codex ids are UUIDv7 and Claude subagent ids share an "agent-" prefix.
const shortIDLen = 12

func shortID(id string) string {
	// A qualified subagent id is "<parent>/agent-xxxx"; the agent half is what
	// distinguishes it from its siblings, so that is the useful short form.
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		id = id[i+1:]
	}
	if len(id) > shortIDLen {
		return id[:shortIDLen]
	}
	return id
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
