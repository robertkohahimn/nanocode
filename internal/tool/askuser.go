package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

// AskUserQuestionTool asks the user structured questions via the terminal.
type AskUserQuestionTool struct {
	StdinReader *bufio.Reader
}

type askInput struct {
	Question string   `json:"question"`
	Options  []option `json:"options"`
}

type option struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type askResponse struct {
	Question string `json:"question"`
	Selected string `json:"selected"`
	IsCustom bool   `json:"is_custom"`
}

func (t *AskUserQuestionTool) Name() string { return "ask_user" }

func (t *AskUserQuestionTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Name:        "ask_user",
		Description: "Ask the user a structured question with options. Use when you need the user to make a choice between specific alternatives.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"question": {"type": "string", "description": "The question to ask the user"},
				"options": {
					"type": "array",
					"description": "Array of options for the user to choose from. If empty, asks as free-form.",
					"items": {
						"type": "object",
						"properties": {
							"label": {"type": "string", "description": "Short label for the option"},
							"description": {"type": "string", "description": "Longer description of the option"}
						},
						"required": ["label"]
					}
				}
			},
			"required": ["question"]
		}`),
	}
}

func (t *AskUserQuestionTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	in, err := ParseInput[askInput](input)
	if err != nil {
		return "", fmt.Errorf("parsing input: %w", err)
	}

	if in.Question == "" {
		return "", fmt.Errorf("question is required")
	}

	reader := t.StdinReader
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}

	resp := askResponse{Question: in.Question}

	if len(in.Options) == 0 {
		resp.Selected = t.askFreeForm(reader, in.Question)
		resp.IsCustom = true
	} else {
		resp.Selected, resp.IsCustom = t.askWithOptions(reader, in.Question, in.Options)
	}

	out, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("marshaling response: %w", err)
	}
	return string(out), nil
}

// askFreeForm prints the question and reads a free-form response.
func (t *AskUserQuestionTool) askFreeForm(reader *bufio.Reader, question string) string {
	fmt.Fprintf(os.Stderr, "\n  \033[1;36m%s\033[0m\n  > ", question)
	line, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(line)
}

// askWithOptions prints numbered options and reads the user's selection.
func (t *AskUserQuestionTool) askWithOptions(reader *bufio.Reader, question string, opts []option) (string, bool) {
	totalOpts := len(opts) + 1 // +1 for "Other"

	t.printOptions(question, opts, totalOpts)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", false
		}
		line = strings.TrimSpace(line)
		if line == "" {
			fmt.Fprintf(os.Stderr, "  Select [1-%d]: ", totalOpts)
			continue
		}

		num, err := strconv.Atoi(line)
		if err != nil || num < 1 || num > totalOpts {
			fmt.Fprintf(os.Stderr, "  \033[33mPlease enter a number between 1 and %d.\033[0m\n  Select [1-%d]: ", totalOpts, totalOpts)
			continue
		}

		// "Other" option selected
		if num == totalOpts {
			fmt.Fprintf(os.Stderr, "  Type your response: ")
			custom, readErr := reader.ReadString('\n')
			if readErr != nil {
				return "", true
			}
			return strings.TrimSpace(custom), true
		}

		return opts[num-1].Label, false
	}
}

// printOptions renders the question and options to stderr.
func (t *AskUserQuestionTool) printOptions(question string, opts []option, totalOpts int) {
	fmt.Fprintf(os.Stderr, "\n  \033[1;36m%s\033[0m\n", question)
	for i, opt := range opts {
		fmt.Fprintf(os.Stderr, "  \033[1m[%d]\033[0m %s\n", i+1, opt.Label)
		if opt.Description != "" {
			fmt.Fprintf(os.Stderr, "      \033[2m%s\033[0m\n", opt.Description)
		}
	}
	fmt.Fprintf(os.Stderr, "  \033[1m[%d]\033[0m Other (type custom response)\n", totalOpts)
	fmt.Fprintf(os.Stderr, "\n  Select [1-%d]: ", totalOpts)
}
