package session

import (
	"bytes"
	"encoding/json"
)

// Transcript lines can carry megabytes of tool output beside a few bytes of
// conversation. encoding/json validates and walks every byte before decoding,
// so these helpers only locate value boundaries; callers decode the values
// they keep with encoding/json. Structure outside kept values is not validated.

// maxScanDepth matches encoding/json's nesting limit.
const maxScanDepth = 10000

// eachMember visits the members of the JSON object in raw until visit
// returns false. It reports false when raw is not a structurally complete
// object, unless visit stopped first.
func eachMember(raw []byte, visit func(key string, value []byte) bool) bool {
	i := skipSpace(raw, 0)
	if i >= len(raw) || raw[i] != '{' {
		return false
	}
	end, ok := scanObject(raw, i, 0, visit)
	return ok && (end < 0 || skipSpace(raw, end) == len(raw))
}

// eachElement visits the values of the JSON array in raw.
func eachElement(raw []byte, visit func(value []byte) bool) bool {
	i := skipSpace(raw, 0)
	if i >= len(raw) || raw[i] != '[' {
		return false
	}
	end, ok := scanArray(raw, i, 0, visit)
	return ok && (end < 0 || skipSpace(raw, end) == len(raw))
}

// scanObject returns the offset after the object at raw[i], or -1 when visit stopped.
func scanObject(raw []byte, i, depth int, visit func(string, []byte) bool) (int, bool) {
	if depth >= maxScanDepth {
		return 0, false
	}
	i = skipSpace(raw, i+1)
	if i < len(raw) && raw[i] == '}' {
		return i + 1, true
	}
	for {
		if i >= len(raw) || raw[i] != '"' {
			return 0, false
		}
		keyStart := i
		keyEnd, ok := skipString(raw, i)
		if !ok {
			return 0, false
		}
		key := raw[keyStart+1 : keyEnd-1]
		i = skipSpace(raw, keyEnd)
		if i >= len(raw) || raw[i] != ':' {
			return 0, false
		}
		start := skipSpace(raw, i+1)
		end, ok := skipValue(raw, start, depth+1)
		if !ok {
			return 0, false
		}
		if visit != nil {
			name := string(key)
			if bytes.IndexByte(key, '\\') >= 0 && json.Unmarshal(raw[keyStart:keyEnd], &name) != nil {
				return 0, false
			}
			if !visit(name, raw[start:end]) {
				return -1, true
			}
		}
		i = skipSpace(raw, end)
		if i >= len(raw) {
			return 0, false
		}
		if raw[i] == '}' {
			return i + 1, true
		}
		if raw[i] != ',' {
			return 0, false
		}
		i = skipSpace(raw, i+1)
	}
}

func scanArray(raw []byte, i, depth int, visit func([]byte) bool) (int, bool) {
	if depth >= maxScanDepth {
		return 0, false
	}
	i = skipSpace(raw, i+1)
	if i < len(raw) && raw[i] == ']' {
		return i + 1, true
	}
	for {
		end, ok := skipValue(raw, i, depth+1)
		if !ok {
			return 0, false
		}
		if visit != nil && !visit(raw[i:end]) {
			return -1, true
		}
		i = skipSpace(raw, end)
		if i >= len(raw) {
			return 0, false
		}
		if raw[i] == ']' {
			return i + 1, true
		}
		if raw[i] != ',' {
			return 0, false
		}
		i = skipSpace(raw, i+1)
	}
}

func skipValue(raw []byte, i, depth int) (int, bool) {
	if i >= len(raw) {
		return 0, false
	}
	switch c := raw[i]; {
	case c == '"':
		return skipString(raw, i)
	case c == '{':
		return scanObject(raw, i, depth, nil)
	case c == '[':
		return scanArray(raw, i, depth, nil)
	case c == 't':
		return skipLiteral(raw, i, "true")
	case c == 'f':
		return skipLiteral(raw, i, "false")
	case c == 'n':
		return skipLiteral(raw, i, "null")
	case c == '-' || '0' <= c && c <= '9':
		j := i + 1
		for j < len(raw) && (raw[j] >= '0' && raw[j] <= '9' || raw[j] == '.' || raw[j] == 'e' || raw[j] == 'E' || raw[j] == '+' || raw[j] == '-') {
			j++
		}
		return j, true
	}
	return 0, false
}

// skipString returns the offset after the string opening at raw[i].
func skipString(raw []byte, i int) (int, bool) {
	j := i + 1
	for {
		k := bytes.IndexByte(raw[j:], '"')
		if k < 0 {
			return 0, false
		}
		j += k
		backslashes := 0
		for b := j - 1; b > i && raw[b] == '\\'; b-- {
			backslashes++
		}
		j++
		if backslashes%2 == 0 {
			return j, true
		}
	}
}

func skipLiteral(raw []byte, i int, literal string) (int, bool) {
	if !bytes.HasPrefix(raw[i:], []byte(literal)) {
		return 0, false
	}
	return i + len(literal), true
}

func skipSpace(raw []byte, i int) int {
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r') {
		i++
	}
	return i
}
