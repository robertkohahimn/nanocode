package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAskUserName(t *testing.T) {
	tool := &AskUserQuestionTool{}
	if tool.Name() != "ask_user" {
		t.Errorf("expected ask_user, got %s", tool.Name())
	}
}

func TestAskUserDefinition(t *testing.T) {
	tool := &AskUserQuestionTool{}
	def := tool.Definition()
	if def.Name != "ask_user" {
		t.Errorf("expected ask_user, got %s", def.Name)
	}
	if def.InputSchema == nil {
		t.Fatal("expected non-nil input schema")
	}
}

func TestAskUserWithOptions(t *testing.T) {
	input := "1\n"
	reader := bufio.NewReader(strings.NewReader(input))
	askTool := &AskUserQuestionTool{StdinReader: reader}

	inp := `{"question": "Pick a color", "options": [{"label": "Red", "description": "A warm color"}, {"label": "Blue", "description": "A cool color"}]}`
	result, err := askTool.Execute(context.Background(), json.RawMessage(inp))
	if err != nil {
		t.Fatal(err)
	}

	var resp askResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Selected != "Red" {
		t.Errorf("expected Red, got %s", resp.Selected)
	}
	if resp.IsCustom {
		t.Error("expected IsCustom=false")
	}
	if resp.Question != "Pick a color" {
		t.Errorf("expected question in response, got %s", resp.Question)
	}
}

func TestAskUserSelectSecondOption(t *testing.T) {
	input := "2\n"
	reader := bufio.NewReader(strings.NewReader(input))
	askTool := &AskUserQuestionTool{StdinReader: reader}

	inp := `{"question": "Pick a color", "options": [{"label": "Red", "description": "A warm color"}, {"label": "Blue", "description": "A cool color"}]}`
	result, err := askTool.Execute(context.Background(), json.RawMessage(inp))
	if err != nil {
		t.Fatal(err)
	}

	var resp askResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Selected != "Blue" {
		t.Errorf("expected Blue, got %s", resp.Selected)
	}
	if resp.IsCustom {
		t.Error("expected IsCustom=false")
	}
}

func TestAskUserOtherOption(t *testing.T) {
	// Select "Other" (option 3 for 2 options), then type custom text
	input := "3\nGreen is my favorite\n"
	reader := bufio.NewReader(strings.NewReader(input))
	askTool := &AskUserQuestionTool{StdinReader: reader}

	inp := `{"question": "Pick a color", "options": [{"label": "Red", "description": "A warm color"}, {"label": "Blue", "description": "A cool color"}]}`
	result, err := askTool.Execute(context.Background(), json.RawMessage(inp))
	if err != nil {
		t.Fatal(err)
	}

	var resp askResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Selected != "Green is my favorite" {
		t.Errorf("expected 'Green is my favorite', got %s", resp.Selected)
	}
	if !resp.IsCustom {
		t.Error("expected IsCustom=true for Other option")
	}
}

func TestAskUserFreeForm(t *testing.T) {
	input := "I want something custom\n"
	reader := bufio.NewReader(strings.NewReader(input))
	askTool := &AskUserQuestionTool{StdinReader: reader}

	inp := `{"question": "What approach do you prefer?"}`
	result, err := askTool.Execute(context.Background(), json.RawMessage(inp))
	if err != nil {
		t.Fatal(err)
	}

	var resp askResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Selected != "I want something custom" {
		t.Errorf("expected 'I want something custom', got %s", resp.Selected)
	}
	if !resp.IsCustom {
		t.Error("expected IsCustom=true for free-form")
	}
}

func TestAskUserInvalidThenValid(t *testing.T) {
	// First type invalid input "abc", then valid "1"
	input := "abc\n1\n"
	reader := bufio.NewReader(strings.NewReader(input))
	askTool := &AskUserQuestionTool{StdinReader: reader}

	inp := `{"question": "Pick one", "options": [{"label": "Alpha"}, {"label": "Beta"}]}`
	result, err := askTool.Execute(context.Background(), json.RawMessage(inp))
	if err != nil {
		t.Fatal(err)
	}

	var resp askResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Selected != "Alpha" {
		t.Errorf("expected Alpha, got %s", resp.Selected)
	}
}

func TestAskUserEmptyQuestion(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("1\n"))
	askTool := &AskUserQuestionTool{StdinReader: reader}

	inp := `{"question": ""}`
	_, err := askTool.Execute(context.Background(), json.RawMessage(inp))
	if err == nil {
		t.Fatal("expected error for empty question")
	}
}

func TestAskUserEmptyInputThenValid(t *testing.T) {
	// User presses Enter (empty), then types "2"
	input := "\n2\n"
	reader := bufio.NewReader(strings.NewReader(input))
	askTool := &AskUserQuestionTool{StdinReader: reader}

	inp := `{"question": "Pick", "options": [{"label": "A"}, {"label": "B"}]}`
	result, err := askTool.Execute(context.Background(), json.RawMessage(inp))
	if err != nil {
		t.Fatal(err)
	}

	var resp askResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Selected != "B" {
		t.Errorf("expected B, got %s", resp.Selected)
	}
}

func TestAskUserOutOfRangeThenValid(t *testing.T) {
	// "99" is out of range, then "1" is valid
	input := "99\n1\n"
	reader := bufio.NewReader(strings.NewReader(input))
	askTool := &AskUserQuestionTool{StdinReader: reader}

	inp := `{"question": "Pick", "options": [{"label": "X"}]}`
	result, err := askTool.Execute(context.Background(), json.RawMessage(inp))
	if err != nil {
		t.Fatal(err)
	}

	var resp askResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Selected != "X" {
		t.Errorf("expected X, got %s", resp.Selected)
	}
}

func TestAskUserEmptyOptions(t *testing.T) {
	// Empty options array should behave as free-form
	input := "my answer\n"
	reader := bufio.NewReader(strings.NewReader(input))
	askTool := &AskUserQuestionTool{StdinReader: reader}

	inp := `{"question": "What?", "options": []}`
	result, err := askTool.Execute(context.Background(), json.RawMessage(inp))
	if err != nil {
		t.Fatal(err)
	}

	var resp askResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Selected != "my answer" {
		t.Errorf("expected 'my answer', got %s", resp.Selected)
	}
	if !resp.IsCustom {
		t.Error("expected IsCustom=true for empty options (free-form)")
	}
}
