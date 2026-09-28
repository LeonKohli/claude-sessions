package session

import (
	"bufio"
	"bytes"
	"io"
	"iter"
)

// Candidate filtering may admit unrelated records; JSON decoding decides their type.
// Unicode escapes can hide a candidate string, so those records always need decoding.
func mayContainJSONStrings(raw []byte, values ...string) bool {
	if bytes.Contains(raw, []byte(`\u`)) {
		return true
	}
	for _, value := range values {
		if bytes.Contains(raw, []byte(value)) {
			return true
		}
	}
	return false
}

// readLines allows embedded tool output to exceed Scanner's token limit.
func readLines(r io.Reader) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		reader := bufio.NewReader(r)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 && !yield(line, nil) {
				return
			}
			if err != nil {
				if err != io.EOF {
					yield(nil, err)
				}
				return
			}
		}
	}
}
