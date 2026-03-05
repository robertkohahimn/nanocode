package provider

import (
	"io"
	"strings"
	"testing"
)

func TestSSEReaderBasicEvent(t *testing.T) {
	input := "data: hello world\n\n"
	r := NewSSEReader(strings.NewReader(input))

	ev, err := r.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Data != "hello world" {
		t.Errorf("expected 'hello world', got %q", ev.Data)
	}
	if ev.Type != "" {
		t.Errorf("expected empty type, got %q", ev.Type)
	}

	_, err = r.Next()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestSSEReaderNamedEvent(t *testing.T) {
	input := "event: content_block_delta\ndata: {\"text\":\"hi\"}\n\n"
	r := NewSSEReader(strings.NewReader(input))

	ev, err := r.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Type != "content_block_delta" {
		t.Errorf("expected type content_block_delta, got %q", ev.Type)
	}
	if ev.Data != `{"text":"hi"}` {
		t.Errorf("unexpected data: %q", ev.Data)
	}
}

func TestSSEReaderMultipleEvents(t *testing.T) {
	input := "data: first\n\ndata: second\n\n"
	r := NewSSEReader(strings.NewReader(input))

	ev1, _ := r.Next()
	if ev1.Data != "first" {
		t.Errorf("expected first, got %q", ev1.Data)
	}

	ev2, _ := r.Next()
	if ev2.Data != "second" {
		t.Errorf("expected second, got %q", ev2.Data)
	}

	_, err := r.Next()
	if err != io.EOF {
		t.Errorf("expected EOF")
	}
}

func TestSSEReaderMultilineData(t *testing.T) {
	input := "data: line1\ndata: line2\ndata: line3\n\n"
	r := NewSSEReader(strings.NewReader(input))

	ev, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Data != "line1\nline2\nline3" {
		t.Errorf("expected multiline data, got %q", ev.Data)
	}
}

func TestSSEReaderCommentSkipped(t *testing.T) {
	input := ": this is a comment\ndata: actual\n\n"
	r := NewSSEReader(strings.NewReader(input))

	ev, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Data != "actual" {
		t.Errorf("expected 'actual', got %q", ev.Data)
	}
}

func TestSSEReaderEmptyEventsSkipped(t *testing.T) {
	input := "\n\n\ndata: real\n\n"
	r := NewSSEReader(strings.NewReader(input))

	ev, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Data != "real" {
		t.Errorf("expected 'real', got %q", ev.Data)
	}
}

func TestSSEReaderDataNoSpace(t *testing.T) {
	input := "data:nospace\n\n"
	r := NewSSEReader(strings.NewReader(input))

	ev, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Data != "nospace" {
		t.Errorf("expected 'nospace', got %q", ev.Data)
	}
}

func TestSSEReaderDoneEvent(t *testing.T) {
	input := "data: [DONE]\n\n"
	r := NewSSEReader(strings.NewReader(input))

	ev, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Data != "[DONE]" {
		t.Errorf("expected [DONE], got %q", ev.Data)
	}
}

func TestSSEReaderNoTrailingNewline(t *testing.T) {
	input := "data: trailing"
	r := NewSSEReader(strings.NewReader(input))

	ev, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Data != "trailing" {
		t.Errorf("expected 'trailing', got %q", ev.Data)
	}
}
