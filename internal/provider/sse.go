package provider

import (
	"bufio"
	"io"
	"strings"
)

// SSEReader reads events from an SSE stream.
type SSEReader struct {
	scanner *bufio.Scanner
}

// NewSSEReader wraps an io.Reader (typically the HTTP response body).
func NewSSEReader(r io.Reader) *SSEReader {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &SSEReader{scanner: s}
}

// SSEEvent is a parsed SSE event.
type SSEEvent struct {
	Type string
	Data string
}

// Next returns the next SSE event. Returns io.EOF when the stream ends.
func (s *SSEReader) Next() (*SSEEvent, error) {
	var eventType string
	var dataLines []string

	for s.scanner.Scan() {
		line := s.scanner.Text()

		if line == "" {
			if len(dataLines) == 0 {
				continue
			}
			data := strings.Join(dataLines, "\n")
			return &SSEEvent{Type: eventType, Data: data}, nil
		}

		if strings.HasPrefix(line, ":") {
			continue
		}

		if after, ok := strings.CutPrefix(line, "event:"); ok {
			eventType = strings.TrimPrefix(after, " ")
			continue
		}

		if after, ok := strings.CutPrefix(line, "data:"); ok {
			dataLines = append(dataLines, strings.TrimPrefix(after, " "))
			continue
		}
	}

	if err := s.scanner.Err(); err != nil {
		return nil, err
	}

	if len(dataLines) > 0 {
		data := strings.Join(dataLines, "\n")
		return &SSEEvent{Type: eventType, Data: data}, nil
	}

	return nil, io.EOF
}
