package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CacheDir follows the platform's user cache location, including XDG_CACHE_HOME.
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "agent-sessions"), nil
}

// CachePath returns the full path to the gob cache file.
func CachePath() (string, error) {
	dir, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "index.gob"), nil
}

// DecodeProjectDirName decodes a Claude projects directory name.
// The encoding is: leading "/" becomes "-", then all "/" become "-".
// But this is ambiguous ("my-project" vs "my/project"), so we only
// use this as a LAST RESORT. Prefer originalPath from sessions-index.json.
func DecodeProjectDirName(name string) string {
	if name == "-" {
		return "/"
	}
	if !strings.HasPrefix(name, "-") {
		return name
	}
	return "/" + strings.ReplaceAll(name[1:], "-", "/")
}

// ResolveProjectPath determines the real project path using the resolution hierarchy:
// 1. originalPath from sessions-index.json (passed in)
// 2. projectPath from session entry (passed in)
// 3. cwd from first JSONL message (passed in)
// 4. Decoded directory name when no recorded path is available
func ResolveProjectPath(originalPath, entryProjectPath, jsonlCWD, dirName string) string {
	if originalPath != "" {
		return originalPath
	}
	if entryProjectPath != "" {
		return entryProjectPath
	}
	if jsonlCWD != "" {
		return jsonlCWD
	}
	return DecodeProjectDirName(dirName)
}

// Truncate truncates a string to maxLen, appending "…" if truncated.
func Truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}
	return string(runes[:maxLen-1]) + "…"
}

// CleanPrompt strips XML-like tags and normalizes whitespace for display.
func CleanPrompt(s string) string {
	// Strip XML-like tags
	result := make([]byte, 0, len(s))
	inTag := false
	for i := 0; i < len(s); i++ {
		if s[i] == '<' {
			inTag = true
			continue
		}
		if s[i] == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result = append(result, s[i])
		}
	}

	// Collapse whitespace
	out := strings.TrimSpace(string(result))
	for strings.Contains(out, "  ") {
		out = strings.ReplaceAll(out, "  ", " ")
	}
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}

	// Take first meaningful line
	lines := strings.SplitN(out, "\n", 3)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 5 {
			return line
		}
	}
	return out
}

// FormatSize formats a byte count to human-readable form.
func FormatSize(bytes int64) string {
	switch {
	case bytes >= 1024*1024:
		return trimTrailingZeros(fmt.Sprintf("%.1f", float64(bytes)/(1024*1024))) + " MB"
	case bytes >= 1024:
		return trimTrailingZeros(fmt.Sprintf("%.1f", float64(bytes)/1024)) + " KB"
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// RelativeTime formats a timestamp as a human-readable relative time.
func RelativeTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	case d < 7*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%dd ago", days)
	case d < 30*24*time.Hour:
		weeks := int(d.Hours() / 24 / 7)
		if weeks == 1 {
			return "1w ago"
		}
		return fmt.Sprintf("%dw ago", weeks)
	default:
		months := int(d.Hours() / 24 / 30)
		if months == 1 {
			return "1mo ago"
		}
		if months < 12 {
			return fmt.Sprintf("%dmo ago", months)
		}
		return t.Format("Jan 2006")
	}
}

// FormatDuration formats a duration for display.
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

// FormatTokens formats token count for compact display.
func FormatTokens(tokens int64) string {
	if tokens <= 0 {
		return "—"
	}
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	if tokens < 1000000 {
		return trimTrailingZeros(fmt.Sprintf("%.1f", float64(tokens)/1000)) + "K"
	}
	return trimTrailingZeros(fmt.Sprintf("%.1f", float64(tokens)/1000000)) + "M"
}

func trimTrailingZeros(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}
