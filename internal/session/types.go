package session

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/LeonKohli/claude-sessions/internal/provider"
)

// SessionEntry holds metadata for a single agent session, from either provider.
type SessionEntry struct {
	Provider     provider.Kind `json:"provider"`
	SessionID    string        `json:"sessionId"`
	FullPath     string        `json:"fullPath"`
	Summary      string        `json:"summary"`
	FirstPrompt  string        `json:"firstPrompt"`
	MessageCount int           `json:"messageCount"`
	Created      time.Time     `json:"created"`
	Modified     time.Time     `json:"modified"`
	GitBranch    string        `json:"gitBranch"`
	ProjectPath  string        `json:"projectPath"`
	IsSidechain  bool          `json:"isSidechain"`
	FileMtime    int64         `json:"fileMtime"`
	FileSize     int64         // populated from os.Stat

	// Child threads: Claude subagent files and Codex spawned/review threads.
	// Hidden from the list unless explicitly revealed.
	IsSubagent bool   `json:"isSubagent"`
	AgentLabel string `json:"agentLabel"` // e.g. "Wegener/explorer"
	Parent     string `json:"parent"`     // session that spawned this thread
	Archived   bool   `json:"archived"`

	// For display
	ShortID string // first 8 chars of UUID

	// Enrichment data (populated lazily on preview)
	TotalInputTokens  int64  // sum of input_tokens across all assistant msgs
	TotalOutputTokens int64  // sum of output_tokens across all assistant msgs
	CacheReadTokens   int64  // sum of cache_read_input_tokens
	CacheWriteTokens  int64  // sum of cache_creation_input_tokens
	Model             string // primary model used (most frequent)
	ToolsUsed         []string
	FilesModified     []string
	Enriched          bool // true once enrichment is done
}

// ReferenceID identifies a physical archive; SessionID remains the resume target.
func (s SessionEntry) ReferenceID() string {
	if s.Provider == provider.Codex {
		name := strings.TrimSuffix(filepath.Base(s.FullPath), ".jsonl")
		prefix, rollout, ok := strings.Cut(name, "_")
		if ok && rollout != "" && strings.HasPrefix(prefix, "rollout-") && strings.HasSuffix(prefix, "-"+s.SessionID) {
			return s.SessionID + "/" + rollout
		}
		if len(s.SessionID) == 36 && strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, "-"+s.SessionID) {
			return s.SessionID + "/" + s.SessionID
		}
	}
	return s.SessionID
}

// Duration returns the wall-clock duration of the session.
func (s SessionEntry) Duration() time.Duration {
	if s.Created.IsZero() || s.Modified.IsZero() {
		return 0
	}
	d := s.Modified.Sub(s.Created)
	if d < 0 {
		return 0
	}
	return d
}

// TotalTokens returns total tokens consumed (input + output).
func (s SessionEntry) TotalTokens() int64 {
	return s.TotalInputTokens + s.TotalOutputTokens
}

// EstimatedMessages uses a completed scan, then index metadata, then a size estimate.
func (s SessionEntry) EstimatedMessages() int {
	if s.Enriched || s.MessageCount > 0 {
		return s.MessageCount
	}
	// Rough estimate: ~3KB per user+assistant message pair
	if s.FileSize > 0 {
		est := int(s.FileSize / 3000)
		if est < 1 {
			est = 1
		}
		return est
	}
	return 0
}

// DisplayTitle returns the best available title for the session.
func (s SessionEntry) DisplayTitle() string {
	if s.Summary != "" {
		return s.Summary
	}
	if s.FirstPrompt != "" {
		return s.FirstPrompt
	}
	return s.SessionID
}

// PreviewMessage is one conversation turn, normalised across both providers.
type PreviewMessage struct {
	Role      string // "user" or "assistant"
	Text      string
	Timestamp time.Time
}

// EnrichmentData holds the metrics extracted by a full transcript scan.
type EnrichmentData struct {
	TotalInputTokens  int64
	TotalOutputTokens int64
	CacheReadTokens   int64
	CacheWriteTokens  int64
	Model             string // most-used model
	ToolsUsed         []string
	FilesModified     []string
	MessageCount      int
}

// SearchableLine is a text line extracted from a session for deep search.
type SearchableLine struct {
	Source    string
	Text      string
	Role      string
	Timestamp time.Time
	LineNum   int
}

// ReadPreview loads up to maxMessages conversation turns for the preview pane.
func ReadPreview(p provider.Kind, path string, maxMessages int) ([]PreviewMessage, error) {
	var messages []PreviewMessage
	err := WalkSearchable(context.Background(), p, path, func(line SearchableLine) bool {
		messages = append(messages, PreviewMessage{Role: line.Role, Text: line.Text, Timestamp: line.Timestamp})
		return maxMessages <= 0 || len(messages) < maxMessages
	})
	return messages, err
}

// Enrich does a full transcript scan to extract tokens, model, tools and files.
func Enrich(p provider.Kind, path string) (*EnrichmentData, error) {
	if p == provider.Codex {
		return EnrichCodexSession(path)
	}
	return EnrichClaudeSession(path)
}

// WalkSearchable visits conversation text until EOF, cancellation, or visit stops.
func WalkSearchable(ctx context.Context, p provider.Kind, path string, visit func(SearchableLine) bool) error {
	if p == provider.Codex {
		return WalkCodexText(ctx, path, visit)
	}
	return WalkClaudeText(ctx, path, visit)
}

// Message represents a single JSONL line from a Claude session file.
type Message struct {
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	UUID      string          `json:"uuid"`
	SessionID string          `json:"sessionId"`
	CWD       string          `json:"cwd"`
	GitBranch string          `json:"gitBranch"`
	Message   *MessageContent `json:"message,omitempty"`
}

// MessageContent holds the role + content from a Claude message.
type MessageContent struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []any containing decoded block objects
	Model   string      `json:"model,omitempty"`
	Usage   *Usage      `json:"usage,omitempty"`
}

// Usage tracks token consumption for a Claude assistant message.
type Usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// SessionsIndex is the structure of Claude's sessions-index.json files.
type SessionsIndex struct {
	Version      int                 `json:"version"`
	OriginalPath string              `json:"originalPath"`
	Entries      []SessionIndexEntry `json:"entries"`
}

// SessionIndexEntry is one entry inside a sessions-index.json.
type SessionIndexEntry struct {
	SessionID    string  `json:"sessionId"`
	FullPath     string  `json:"fullPath"`
	FileMtime    float64 `json:"fileMtime"` // ms timestamp
	FirstPrompt  string  `json:"firstPrompt"`
	Summary      string  `json:"summary"`
	MessageCount int     `json:"messageCount"`
	Created      string  `json:"created"`  // ISO 8601
	Modified     string  `json:"modified"` // ISO 8601
	GitBranch    string  `json:"gitBranch"`
	ProjectPath  string  `json:"projectPath"`
	IsSidechain  bool    `json:"isSidechain"`
}
