package session

import "time"

// SessionEntry holds metadata for a single Claude Code session.
type SessionEntry struct {
	SessionID    string    `json:"sessionId"`
	FullPath     string    `json:"fullPath"`
	Summary      string    `json:"summary"`
	FirstPrompt  string    `json:"firstPrompt"`
	MessageCount int       `json:"messageCount"`
	Created      time.Time `json:"created"`
	Modified     time.Time `json:"modified"`
	GitBranch    string    `json:"gitBranch"`
	ProjectPath  string    `json:"projectPath"`
	IsSidechain  bool      `json:"isSidechain"`
	FileMtime    int64     `json:"fileMtime"`
	FileSize     int64     // populated from os.Stat

	// For display
	ShortID string // first 8 chars of UUID

	// Enrichment data (populated by background scan or estimation)
	TotalInputTokens  int64  // sum of input_tokens across all assistant msgs
	TotalOutputTokens int64  // sum of output_tokens across all assistant msgs
	CacheReadTokens   int64  // sum of cache_read_input_tokens
	CacheWriteTokens  int64  // sum of cache_creation_input_tokens
	Model             string // primary model used (most frequent)
	ToolsUsed         []string
	FilesModified     []string
	Enriched          bool // true once background enrichment is done
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

// EstimatedMessages returns MessageCount if known, else estimates from file size.
func (s SessionEntry) EstimatedMessages() int {
	if s.MessageCount > 0 {
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

// Message represents a single JSONL line from a session file.
type Message struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	UUID      string    `json:"uuid"`
	SessionID string    `json:"sessionId"`
	CWD       string    `json:"cwd"`
	GitBranch string    `json:"gitBranch"`
	Message   *MessageContent `json:"message,omitempty"`
}

// MessageContent holds the role + content from a message.
type MessageContent struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []ContentBlock
	Model   string      `json:"model,omitempty"`
	Usage   *Usage      `json:"usage,omitempty"`
}

// Usage tracks token consumption for an assistant message.
type Usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// ContentBlock is one element in an assistant's content array.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"` // for tool_use blocks
}

// SessionsIndex is the structure of sessions-index.json files.
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

// DeepSearchResult holds a match from deep content search.
type DeepSearchResult struct {
	SessionID string
	Line      string
	LineNum   int
	Timestamp time.Time
	Role      string
}
