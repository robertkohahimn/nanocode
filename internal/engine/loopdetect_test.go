package engine

import (
	"strings"
	"testing"
)

func TestCheckEdit_Oscillation(t *testing.T) {
	ld := NewLoopDetector()
	// A -> B -> A should trigger oscillation
	w := ld.CheckEdit("foo.go", "content A")
	if w != nil {
		t.Fatalf("first edit should not warn, got %v", w)
	}
	w = ld.CheckEdit("foo.go", "content B")
	if w != nil {
		t.Fatalf("second edit should not warn, got %v", w)
	}
	w = ld.CheckEdit("foo.go", "content A")
	if w == nil {
		t.Fatal("third edit (A->B->A) should trigger oscillation warning")
	}
	if w.Type != "oscillation" {
		t.Fatalf("expected oscillation, got %s", w.Type)
	}
}

func TestCheckEdit_NoOscillation(t *testing.T) {
	ld := NewLoopDetector()
	// A -> B -> C should NOT trigger oscillation
	ld.CheckEdit("foo.go", "content A")
	ld.CheckEdit("foo.go", "content B")
	w := ld.CheckEdit("foo.go", "content C")
	// C is unique, no repeated edit, only 3 edits (under limit)
	if w != nil {
		t.Fatalf("A->B->C should not trigger, got %v", w)
	}
}

func TestCheckEdit_RepeatedEdit(t *testing.T) {
	ld := NewLoopDetector()
	w := ld.CheckEdit("foo.go", "same content")
	if w != nil {
		t.Fatalf("first edit should not warn, got %v", w)
	}
	w = ld.CheckEdit("foo.go", "same content")
	if w == nil {
		t.Fatal("second identical edit should trigger repeated_edit")
	}
	if w.Type != "repeated_edit" {
		t.Fatalf("expected repeated_edit, got %s", w.Type)
	}
}

func TestCheckEdit_EditCount(t *testing.T) {
	ld := NewLoopDetector()
	// 5 edits with unique content should be fine
	for i := 0; i < 5; i++ {
		w := ld.CheckEdit("foo.go", strings.Repeat("x", i+1))
		if w != nil {
			t.Fatalf("edit %d should not warn, got %v", i+1, w)
		}
	}
	// 6th edit should trigger edit_count
	w := ld.CheckEdit("foo.go", "unique content 6")
	if w == nil {
		t.Fatal("6th edit should trigger edit_count warning")
	}
	if w.Type != "edit_count" {
		t.Fatalf("expected edit_count, got %s", w.Type)
	}
}

func TestCheckEdit_SeparateFiles(t *testing.T) {
	ld := NewLoopDetector()
	// Edits to different files should not interfere
	ld.CheckEdit("a.go", "content")
	w := ld.CheckEdit("b.go", "content")
	if w != nil {
		t.Fatalf("different files with same content should not trigger, got %v", w)
	}
}

func TestCheckError_RepeatedErrors(t *testing.T) {
	ld := NewLoopDetector()
	errMsg := "error at main.go:42: undefined variable foo"
	w := ld.CheckError(errMsg)
	if w != nil {
		t.Fatalf("first error should not warn, got %v", w)
	}
	w = ld.CheckError(errMsg)
	if w != nil {
		t.Fatalf("second error should not warn, got %v", w)
	}
	w = ld.CheckError(errMsg)
	if w == nil {
		t.Fatal("third identical error should trigger repeated_error")
	}
	if w.Type != "repeated_error" {
		t.Fatalf("expected repeated_error, got %s", w.Type)
	}
}

func TestCheckError_DifferentErrors(t *testing.T) {
	ld := NewLoopDetector()
	ld.CheckError("error: undefined variable foo")
	ld.CheckError("error: syntax error near bracket")
	w := ld.CheckError("error: type mismatch in assignment")
	if w != nil {
		t.Fatalf("different errors should not trigger, got %v", w)
	}
}

func TestCheckError_NormalizedLineNumbers(t *testing.T) {
	ld := NewLoopDetector()
	// Same error with different line numbers should still match after normalization
	ld.CheckError("main.go:10: undefined: foo")
	ld.CheckError("main.go:25: undefined: foo")
	w := ld.CheckError("main.go:99: undefined: foo")
	if w == nil {
		t.Fatal("same error with different line numbers should match after normalization")
	}
	if w.Type != "repeated_error" {
		t.Fatalf("expected repeated_error, got %s", w.Type)
	}
}

func TestCheckCommand_Repeated(t *testing.T) {
	ld := NewLoopDetector()
	cmd := "go test ./..."
	w := ld.CheckCommand(cmd)
	if w != nil {
		t.Fatalf("first command should not warn, got %v", w)
	}
	w = ld.CheckCommand(cmd)
	if w != nil {
		t.Fatalf("second command should not warn, got %v", w)
	}
	w = ld.CheckCommand(cmd)
	if w == nil {
		t.Fatal("third identical command should trigger repeated_command")
	}
	if w.Type != "repeated_command" {
		t.Fatalf("expected repeated_command, got %s", w.Type)
	}
}

func TestCheckCommand_DifferentCommands(t *testing.T) {
	ld := NewLoopDetector()
	ld.CheckCommand("go test ./...")
	ld.CheckCommand("go build ./...")
	w := ld.CheckCommand("go vet ./...")
	if w != nil {
		t.Fatalf("different commands should not trigger, got %v", w)
	}
}

func TestCheckCommand_WindowedCheck(t *testing.T) {
	ld := NewLoopDetector()
	// Fill with 10 different commands to push older repeats out of the window
	for i := 0; i < 10; i++ {
		ld.CheckCommand(strings.Repeat("cmd", i+1))
	}
	// Now the older entries are outside the last-10 window
	ld.CheckCommand("go test ./...")
	ld.CheckCommand("go test ./...")
	w := ld.CheckCommand("go test ./...")
	if w == nil {
		t.Fatal("3 repeats in last 10 should still trigger")
	}
}

func TestNormalizeError(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"main.go:42: error", "main.go:: error"},
		{"at 0x1a2b3c: segfault", "at : segfault"},
		{"ERROR  at  line  5", "error at line"},
	}
	for _, tc := range tests {
		got := normalizeError(tc.input)
		if got != tc.expected {
			t.Errorf("normalizeError(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestFormatWarning(t *testing.T) {
	w := &LoopWarning{Type: "oscillation", Detail: "test detail"}
	result := FormatWarning(w)
	if !strings.Contains(result, "oscillation") {
		t.Error("formatted warning should contain the type")
	}
	if !strings.Contains(result, "test detail") {
		t.Error("formatted warning should contain the detail")
	}
	if !strings.Contains(result, "fundamentally different") {
		t.Error("formatted warning should contain intervention guidance")
	}
}

func TestErrorHistoryBounded(t *testing.T) {
	ld := NewLoopDetector()
	for i := 0; i < 30; i++ {
		ld.CheckError(strings.Repeat("e", i+1))
	}
	if len(ld.errorHistory) > 20 {
		t.Fatalf("error history should be bounded to 20, got %d", len(ld.errorHistory))
	}
}

func TestCmdHistoryBounded(t *testing.T) {
	ld := NewLoopDetector()
	for i := 0; i < 30; i++ {
		ld.CheckCommand(strings.Repeat("c", i+1))
	}
	if len(ld.cmdHistory) > 20 {
		t.Fatalf("cmd history should be bounded to 20, got %d", len(ld.cmdHistory))
	}
}
