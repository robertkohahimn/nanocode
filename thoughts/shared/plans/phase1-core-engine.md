

# Implementation Plan: Phase 1 -- Core Engine MVP

Generated: 2026-03-05

## Goal

Build the Nanocode core engine: a lean Go coding agent that streams LLM responses,
dispatches tool calls, persists conversations to SQLite, and runs as a single binary
under 3,000 lines total. Phase 1 produces a working CLI that accepts a prompt and
runs an agentic loop until the task is complete.

---

## Go Module Structure

```
nanocode/
  go.mod
  go.sum
  DEPENDENCIES.md
  main.go                         # CLI entry point + REPL (~110 lines)
  internal/
    config/
      config.go                   # JSON config loading (~120 lines)
      config_test.go
    provider/
      provider.go                 # Interface + shared types (~100 lines)
      sse.go                      # Shared SSE line parser (~80 lines)
      sse_test.go
      anthropic.go                # Anthropic Messages API (~220 lines)
      anthropic_test.go
      openai.go                   # OpenAI Chat Completions API (~200 lines)
      openai_test.go
    engine/
      engine.go                   # Conversation loop (~250 lines)
      engine_test.go
      tools.go                    # Tool registry + dispatch (~100 lines)
      tools_test.go
    tool/
      bash.go                     # Shell execution (~120 lines)
      bash_test.go
      read.go                     # File reading (~80 lines)
      read_test.go
      write.go                    # File writing (~80 lines)
      write_test.go
      edit.go                     # String-replace editing (~130 lines)
      edit_test.go
      glob.go                     # File pattern matching (~80 lines)
      glob_test.go
      grep.go                     # Content search (~100 lines)
      grep_test.go
      subagent.go                 # Spawn sub-conversation (~120 lines)
      subagent_test.go
      tool.go                     # Tool interface + shared types (~60 lines)
      tool_test.go
    store/
      store.go                    # SQLite persistence (~180 lines)
      store_test.go
      migrate.go                  # Schema migrations (~60 lines)
```

**Estimated total: ~2,420 lines of production code** (with tests: ~3,720 lines)

---

## Interface Definitions

### Provider Interface (`internal/provider/provider.go`)

```go
package provider

import (
    "context"
    "encoding/json"
)

// EventType enumerates the kinds of streaming events.
type EventType int

const (
    EventTextDelta EventType = iota
    EventToolCallStart
    EventToolCallDelta
    EventToolCallEnd
    EventUsage
    EventDone
    EventError
)

// Provider streams LLM responses.
type Provider interface {
    // Stream sends a request and returns a channel of events.
    // The channel is closed when the response is complete.
    // The caller must drain the channel.
    Stream(ctx context.Context, req *Request) (<-chan Event, error)

    // Name returns the provider identifier (e.g. "anthropic", "openai").
    Name() string
}

// Request is the unified request format for all providers.
type Request struct {
    Model     string
    Messages  []Message
    Tools     []ToolDef
    MaxTokens int
    System    string
}

// Message represents a conversation message.
type Message struct {
    Role    Role
    Content []ContentBlock
}

// Role is the message author.
type Role string

const (
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
)

// ContentBlock is a polymorphic content element within a message.
// Exactly one of Text, ToolCall, or ToolResult is set based on Type.
type ContentBlock struct {
    Type       string      // "text", "tool_use", "tool_result"
    Text       string      // for Type == "text"
    ToolCall   *ToolCall   // for Type == "tool_use"
    ToolResult *ToolResult // for Type == "tool_result"
}

// ToolCall represents the LLM requesting a tool invocation.
type ToolCall struct {
    ID    string
    Name  string
    Input json.RawMessage // tool-specific JSON
}

// ToolResult is the response to a tool call.
type ToolResult struct {
    ToolCallID string
    Content    string
    IsError    bool
}

// ToolDef describes a tool the LLM can invoke.
type ToolDef struct {
    Name        string
    Description string
    InputSchema json.RawMessage // JSON Schema
}

// Event is a single streaming event from the provider.
type Event struct {
    Type     EventType
    Text     string
    ToolCall *ToolCall
    Usage    *Usage
    Error    error
}

// Usage tracks token consumption.
type Usage struct {
    InputTokens  int
    OutputTokens int
    CacheRead    int
    CacheCreate  int
}
```

### Tool Interface (`internal/tool/tool.go`)

```go
package tool

import (
    "context"
    "encoding/json"

    "github.com/nanocode/nanocode/internal/provider"
)

// Tool is the interface every built-in tool implements.
type Tool interface {
    // Name returns the tool identifier (matches ToolDef.Name).
    Name() string

    // Definition returns the JSON Schema tool definition sent to the LLM.
    Definition() provider.ToolDef

    // Execute runs the tool with the given JSON input.
    // Returns the result string or an error.
    Execute(ctx context.Context, input json.RawMessage) (string, error)
}

// ParseInput unmarshals JSON input into the target struct.
func ParseInput[T any](input json.RawMessage) (T, error) {
    var v T
    err := json.Unmarshal(input, &v)
    return v, err
}

// TruncateOutput caps tool output at maxLen characters.
func TruncateOutput(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen] + "\n... (truncated)"
}
```

### Store Interface (`internal/store/store.go`)

```go
package store

import "context"

// Store handles persistence of sessions and messages.
type Store interface {
    CreateSession(ctx context.Context, project string) (string, error)
    GetSession(ctx context.Context, id string) (*Session, error)
    ListSessions(ctx context.Context, project string, limit int) ([]Session, error)
    AppendMessage(ctx context.Context, sessionID string, msg *MessageRecord) error
    GetMessages(ctx context.Context, sessionID string) ([]MessageRecord, error)
    UpdateSessionTitle(ctx context.Context, id, title string) error
    Close() error
}

type Session struct {
    ID        string
    Project   string
    Title     string
    CreatedAt int64
    UpdatedAt int64
}

type MessageRecord struct {
    ID        string
    SessionID string
    Role      string
    Content   string // JSON-encoded []ContentBlock
    Metadata  string // JSON: {model, usage, duration_ms}
    CreatedAt int64
}
```

### Config (`internal/config/config.go`)

```go
package config

// Config is the top-level configuration structure.
type Config struct {
    Provider   string                `json:"provider"`
    Model      string                `json:"model"`
    APIKey     string                `json:"apiKey"`
    MaxTokens  int                   `json:"maxTokens"`
    System     string                `json:"system"`
    Tools      map[string]ToolConfig `json:"tools"`
    BaseURL    string                `json:"baseURL"`
    ProjectDir string                `json:"-"` // set by Load(), not from JSON
}

type ToolConfig struct {
    Allow []string `json:"allow"`
    Deny  []string `json:"deny"`
}

// Load reads config from nanocode.json (project root) and
// ~/.config/nanocode/config.json (global), merging them.
// Project config overrides global config.
// Environment variables in values are expanded ($VAR or ${VAR}).
func Load(projectDir string) (*Config, error)

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config
```

### Engine (`internal/engine/engine.go`)

```go
package engine

import (
    "context"

    "github.com/nanocode/nanocode/internal/config"
    "github.com/nanocode/nanocode/internal/provider"
    "github.com/nanocode/nanocode/internal/store"
)

// Engine is the core conversation loop.
type Engine struct {
    provider provider.Provider
    tools    *ToolRegistry
    store    store.Store
    config   *config.Config
}

// New creates an Engine with the given dependencies.
func New(p provider.Provider, s store.Store, cfg *config.Config) *Engine

// Run starts a conversation from the user's initial prompt.
// It loops: send messages -> stream response -> collect tool calls ->
// execute tools -> append results -> repeat until no tool calls remain.
// The callback receives streaming events for display.
func (e *Engine) Run(ctx context.Context, sessionID string, prompt string, onEvent func(provider.Event)) error

// Resume continues an existing session with a new user message.
func (e *Engine) Resume(ctx context.Context, sessionID string, prompt string, onEvent func(provider.Event)) error
```

---

## SQL Schema

```sql
-- Applied by migrate.go on first run via PRAGMA user_version.

-- Version 1: initial schema
CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    project    TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project);
CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(updated_at DESC);

CREATE TABLE IF NOT EXISTS messages (
    id         TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    role       TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
    content    TEXT NOT NULL,  -- JSON-encoded []ContentBlock
    metadata   TEXT NOT NULL DEFAULT '{}',  -- JSON: {model, usage, duration_ms}
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at);
```

Note: The `snapshots` table is deferred to Phase 2 (git snapshot tracking).

---

## Dependency List (`go.mod` entries)

| Module | Version | Justification |
|--------|---------|---------------|
| `modernc.org/sqlite` | latest | Pure-Go SQLite driver. No CGo. Required for single-binary constraint. |
| `github.com/google/uuid` | v1 | Session/message ID generation. Single file, zero transitive deps. |

That is the complete external dependency list for Phase 1. Everything else is stdlib:

- `net/http` -- HTTP client for provider APIs
- `database/sql` -- SQLite access layer
- `encoding/json` -- JSON marshaling
- `path/filepath` -- Glob tool + config path resolution
- `os/exec` -- Bash tool, subagent
- `regexp` -- Grep tool
- `bufio` -- SSE line scanning, bash confirmation prompt
- `strings`, `bytes`, `fmt`, `io`, `os`, `context`, `time`, `sync`

`mvdan.cc/sh/v3/syntax` is deferred to Phase 2 (permission system). In Phase 1,
bash commands run with a simple Y/n confirmation prompt on stderr.

---

## Implementation Order

Build order follows the dependency graph. Each step produces a testable unit.

```
Step 1: config        (no internal deps)
Step 2: provider      (no internal deps)
Step 3: store         (no internal deps)
Step 4: tool          (no internal deps)
Step 5: engine        (depends on provider, store, tool)
Step 6: main.go       (depends on all)
```

### Detailed Build Order

```
 1. internal/config/config.go        -- Load, expand env vars, merge
 2. internal/config/config_test.go   -- Test loading, merging, env expansion
 3. internal/provider/provider.go    -- Types + interfaces (no logic)
 4. internal/provider/sse.go         -- SSE line parser
 5. internal/provider/sse_test.go    -- Test SSE parsing with mock data
 6. internal/provider/anthropic.go   -- Anthropic provider
 7. internal/provider/anthropic_test.go -- Test with recorded SSE responses
 8. internal/provider/openai.go      -- OpenAI provider
 9. internal/provider/openai_test.go -- Test with recorded SSE responses
10. internal/store/migrate.go        -- Schema creation
11. internal/store/store.go          -- SQLite store implementation
12. internal/store/store_test.go     -- Test with in-memory SQLite
13. internal/tool/tool.go            -- Interface + shared helpers
14. internal/tool/read.go            -- Read tool (simplest)
15. internal/tool/read_test.go
16. internal/tool/write.go           -- Write tool
17. internal/tool/write_test.go
18. internal/tool/edit.go            -- Edit tool (string replacement)
19. internal/tool/edit_test.go
20. internal/tool/glob.go            -- Glob tool
21. internal/tool/glob_test.go
22. internal/tool/grep.go            -- Grep tool
23. internal/tool/grep_test.go
24. internal/tool/bash.go            -- Bash tool (with Y/n prompt)
25. internal/tool/bash_test.go
26. internal/tool/subagent.go        -- Subagent tool
27. internal/tool/subagent_test.go
28. internal/engine/tools.go         -- Tool registry
29. internal/engine/tools_test.go
30. internal/engine/engine.go        -- Conversation loop
31. internal/engine/engine_test.go   -- Test with mock provider
32. main.go                          -- CLI wiring
```

---

## File-by-File Breakdown

### `main.go` (~80 lines)

**Purpose:** CLI entry point. Parses args, loads config, creates provider/store/engine,
runs the conversation loop, prints streamed output to stdout.

**Key functions:**
```go
func main()
func run() error           // testable main logic
func parseArgs(args []string) (prompt, sessionID string, listMode bool, modelOverride string)
func detectProject() string // walk up directories to find .git or nanocode.json
func xdgDataHome() string  // XDG_DATA_HOME or ~/.local/share
```

**CLI behavior:**
```
nanocode "fix the bug in auth.go"           # single-shot: run prompt, exit
nanocode                                    # interactive REPL mode
nanocode --session <id>                     # resume session in REPL mode
nanocode --session <id> "now add tests"     # resume with prompt, then REPL
nanocode --list                             # list sessions
nanocode --model claude-opus-4-20250514 "refactor"  # override model
```

**Args parsing:** Use `os.Args` directly. No flag library needed for 4 flags.
Walk `os.Args[1:]` linearly: if an arg starts with `--`, consume the flag and
its value. Everything else is the prompt. If no prompt and no `--list`, print
usage and exit.

**Flow:**
```go
func run() error {
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer cancel()

    prompt, sessionID, listMode, modelOverride := parseArgs(os.Args[1:])
    projectDir := detectProject()

    cfg, err := config.Load(projectDir)
    if err != nil {
        return fmt.Errorf("loading config: %w", err)
    }
    if modelOverride != "" {
        cfg.Model = modelOverride
    }

    if cfg.APIKey == "" {
        return fmt.Errorf("no API key configured. Set %s_API_KEY or add apiKey to config", strings.ToUpper(cfg.Provider))
    }

    var prov provider.Provider
    switch cfg.Provider {
    case "anthropic":
        prov = provider.NewAnthropic(cfg.APIKey, cfg.BaseURL)
    case "openai":
        prov = provider.NewOpenAI(cfg.APIKey, cfg.BaseURL)
    default:
        return fmt.Errorf("unknown provider: %s (supported: anthropic, openai)", cfg.Provider)
    }

    dbPath := filepath.Join(xdgDataHome(), "nanocode", "nanocode.db")
    st, err := store.Open(dbPath)
    if err != nil {
        return fmt.Errorf("opening database: %w", err)
    }
    defer st.Close()

    if listMode {
        sessions, err := st.ListSessions(ctx, projectDir, 20)
        if err != nil {
            return fmt.Errorf("listing sessions: %w", err)
        }
        for _, s := range sessions {
            fmt.Fprintf(os.Stdout, "%s  %s  %s\n", s.ID[:8], time.Unix(s.UpdatedAt, 0).Format("2006-01-02 15:04"), s.Title)
        }
        return nil
    }

    eng := engine.New(prov, st, cfg)
    onEvent := func(ev provider.Event) {
        if ev.Type == provider.EventTextDelta {
            fmt.Print(ev.Text)
        }
    }

    // Create or reuse session
    if sessionID == "" {
        sessionID, err = st.CreateSession(ctx, projectDir)
        if err != nil {
            return fmt.Errorf("creating session: %w", err)
        }
    }

    // If a prompt was provided on the command line, run it first
    if prompt != "" {
        if err := eng.Run(ctx, sessionID, prompt, onEvent); err != nil {
            return err
        }
        fmt.Println()
    }

    // If no prompt was given, or after running the initial prompt,
    // enter interactive REPL mode (unless stdin is not a terminal).
    if !isTerminal(os.Stdin) && prompt != "" {
        return nil // piped input + prompt = single-shot mode
    }

    // Interactive REPL loop
    reader := bufio.NewReader(os.Stdin)
    for {
        fmt.Fprintf(os.Stderr, "\n\033[36m>\033[0m ")
        line, err := reader.ReadString('\n')
        if err != nil {
            return nil // EOF = clean exit
        }
        line = strings.TrimSpace(line)
        if line == "" {
            continue
        }
        if line == "exit" || line == "quit" {
            return nil
        }

        if err := eng.Resume(ctx, sessionID, line, onEvent); err != nil {
            fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
            // Don't exit on error — let user try again
        }
        fmt.Println()
    }
}

// isTerminal checks if the file is a terminal (not piped).
func isTerminal(f *os.File) bool {
    info, err := f.Stat()
    if err != nil {
        return false
    }
    return info.Mode()&os.ModeCharDevice != 0
}

func detectProject() string {
    dir, _ := os.Getwd()
    for {
        if _, err := os.Stat(filepath.Join(dir, "nanocode.json")); err == nil {
            return dir
        }
        if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
            return dir
        }
        parent := filepath.Dir(dir)
        if parent == dir {
            break
        }
        dir = parent
    }
    cwd, _ := os.Getwd()
    return cwd
}

func xdgDataHome() string {
    if v := os.Getenv("XDG_DATA_HOME"); v != "" {
        return v
    }
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".local", "share")
}
```

**Test strategy:** Integration test in `main_test.go` that calls `run()` with
a mock provider injected via a test helper. Verify stdout output and database state.

---

### `internal/config/config.go` (~120 lines)

**Purpose:** Load and merge JSON configuration from two locations.

**Key functions:**
```go
func Load(projectDir string) (*Config, error) {
    cfg := DefaultConfig()
    cfg.ProjectDir = projectDir // MUST set before return — used by engine for nanocode.md

    // Load global config
    globalPath := filepath.Join(xdgConfigHome(), "nanocode", "config.json")
    if global, err := loadFile(globalPath); err == nil {
        cfg = merge(cfg, global)
        cfg.ProjectDir = projectDir // preserve after merge
    }

    // Load project config (overrides global)
    if projectDir != "" {
        projectPath := filepath.Join(projectDir, "nanocode.json")
        if project, err := loadFile(projectPath); err == nil {
            cfg = merge(cfg, project)
            cfg.ProjectDir = projectDir // preserve after merge
        }
    }

    expandEnv(cfg)
    return cfg, nil
}

func loadFile(path string) (*Config, error)
func merge(global, project *Config) *Config
func expandEnv(cfg *Config)
func DefaultConfig() *Config
```

**Config search order:**
1. `<projectDir>/nanocode.json`
2. `~/.config/nanocode/config.json` (or `$XDG_CONFIG_HOME/nanocode/config.json`)
3. Built-in defaults

**Merge semantics:** Start with `DefaultConfig()`. Load global config and overlay
non-zero fields. Load project config and overlay non-zero fields. For the `Tools`
map, project entries override global entries per-tool-name (not per-field within
a tool -- the entire ToolConfig is replaced).

```go
func merge(base, overlay *Config) *Config {
    if overlay.Provider != "" {
        base.Provider = overlay.Provider
    }
    if overlay.Model != "" {
        base.Model = overlay.Model
    }
    if overlay.APIKey != "" {
        base.APIKey = overlay.APIKey
    }
    if overlay.MaxTokens != 0 {
        base.MaxTokens = overlay.MaxTokens
    }
    if overlay.System != "" {
        base.System = overlay.System
    }
    if overlay.BaseURL != "" {
        base.BaseURL = overlay.BaseURL
    }
    if overlay.Tools != nil {
        if base.Tools == nil {
            base.Tools = make(map[string]ToolConfig)
        }
        for k, v := range overlay.Tools {
            base.Tools[k] = v
        }
    }
    return base
}
```

**Env expansion:** After merging, walk the string fields and call `os.ExpandEnv`.
This handles `"apiKey": "$ANTHROPIC_API_KEY"`.

```go
func expandEnv(cfg *Config) {
    cfg.APIKey = os.ExpandEnv(cfg.APIKey)
    cfg.BaseURL = os.ExpandEnv(cfg.BaseURL)
    cfg.System = os.ExpandEnv(cfg.System)
}
```

**Default config:**
```go
func DefaultConfig() *Config {
    return &Config{
        Provider:  "anthropic",
        Model:     "claude-sonnet-4-20250514",
        MaxTokens: 8192,
    }
}
```

**XDG config home helper:**
```go
func xdgConfigHome() string {
    if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
        return v
    }
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".config")
}
```

**Test strategy:**
- Write temp JSON files with `os.WriteFile` in `t.TempDir()`, verify loading.
- Test merge: global sets provider, project overrides model, result has both.
- Test env var expansion with `t.Setenv("TEST_KEY", "sk-test")`.
- Test missing files return defaults without error (not an error condition).
- Test malformed JSON returns clear error with file path in message.
- Test project overrides global for every field.

---

### `internal/provider/provider.go` (~100 lines)

**Purpose:** All shared types and interfaces. No logic, no functions with bodies.

Contains all types listed in the Interface Definitions section above:
`EventType` constants, `Provider` interface, `Request`, `Message`, `Role`,
`ContentBlock`, `ToolCall`, `ToolResult`, `ToolDef`, `Event`, `Usage`.

**Test strategy:** No logic to test. All types are exercised by provider and engine tests.

---

### `internal/provider/sse.go` (~80 lines)

**Purpose:** Parse SSE (Server-Sent Events) text/event-stream responses.
Shared by both Anthropic and OpenAI providers.

**Key type and functions:**
```go
// SSEReader reads events from an SSE stream.
type SSEReader struct {
    scanner *bufio.Scanner
}

// NewSSEReader wraps an io.Reader (typically the HTTP response body).
func NewSSEReader(r io.Reader) *SSEReader {
    s := bufio.NewScanner(r)
    s.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line
    return &SSEReader{scanner: s}
}

// SSEEvent is a parsed SSE event.
type SSEEvent struct {
    Type string // from "event:" line, empty if not set
    Data string // from "data:" line(s), joined with newline
}

// Next returns the next SSE event.
// Returns io.EOF when the stream ends.
// Skips comment lines (starting with ':').
// An event is complete when a blank line is encountered.
func (s *SSEReader) Next() (*SSEEvent, error) {
    var eventType string
    var dataLines []string

    for s.scanner.Scan() {
        line := s.scanner.Text()

        // Blank line = event boundary
        if line == "" {
            if len(dataLines) == 0 {
                continue // empty event, skip
            }
            data := strings.Join(dataLines, "\n")
            return &SSEEvent{Type: eventType, Data: data}, nil
        }

        // Comment line
        if strings.HasPrefix(line, ":") {
            continue
        }

        // Event type
        if after, ok := strings.CutPrefix(line, "event:"); ok {
            eventType = strings.TrimPrefix(after, " ")
            continue
        }

        // Data line — SSE spec: colon may be followed by optional single space.
        // Handle both "data: foo" and "data:foo".
        if after, ok := strings.CutPrefix(line, "data:"); ok {
            dataLines = append(dataLines, strings.TrimPrefix(after, " "))
            continue
        }
    }

    if err := s.scanner.Err(); err != nil {
        return nil, err
    }

    // End of stream -- emit final event if data accumulated
    if len(dataLines) > 0 {
        data := strings.Join(dataLines, "\n")
        return &SSEEvent{Type: eventType, Data: data}, nil
    }

    return nil, io.EOF
}
```

**Test strategy:**
- Feed complete SSE stream with multiple events, verify each parsed correctly.
- Test `event:` + `data:` combination (Anthropic style).
- Test `data:`-only events (OpenAI style).
- Test multi-line `data:` fields (multiple `data:` lines before blank line).
- Test comment lines (`:this is a comment`) are skipped.
- Test empty events (blank line with no prior data) are skipped.
- Test `data: [DONE]` is returned as data (caller decides what to do with it).
- Test large data lines (up to 1MB).
- Test scanner error propagation.

---

### `internal/provider/anthropic.go` (~220 lines)

**Purpose:** Implement the Provider interface for the Anthropic Messages API.

**Struct and constructor:**
```go
type Anthropic struct {
    apiKey  string
    baseURL string
    client  *http.Client
}

func NewAnthropic(apiKey, baseURL string) *Anthropic {
    if baseURL == "" {
        baseURL = "https://api.anthropic.com"
    }
    return &Anthropic{
        apiKey:  apiKey,
        baseURL: strings.TrimRight(baseURL, "/"),
        client: &http.Client{
            // Do NOT set Timeout — it covers the entire response body read
            // and will kill long-running SSE streams. Use transport-level
            // timeouts for connection establishment only.
            Transport: &http.Transport{
                ResponseHeaderTimeout: 30 * time.Second,
                TLSHandshakeTimeout:   10 * time.Second,
                DialContext: (&net.Dialer{
                    Timeout: 10 * time.Second,
                }).DialContext,
            },
        },
    }
}

func (a *Anthropic) Name() string { return "anthropic" }
```

**Stream implementation outline:**
```go
func (a *Anthropic) Stream(ctx context.Context, req *Request) (<-chan Event, error) {
    body, err := a.buildRequestBody(req)
    if err != nil {
        return nil, fmt.Errorf("building request: %w", err)
    }

    httpReq, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/v1/messages", bytes.NewReader(body))
    if err != nil {
        return nil, fmt.Errorf("creating request: %w", err)
    }
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("x-api-key", a.apiKey)
    httpReq.Header.Set("anthropic-version", "2023-06-01")

    resp, err := a.client.Do(httpReq)
    if err != nil {
        return nil, fmt.Errorf("sending request: %w", err)
    }

    if resp.StatusCode != http.StatusOK {
        defer resp.Body.Close()
        errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
        return nil, fmt.Errorf("anthropic API error %d: %s", resp.StatusCode, string(errBody))
    }

    ch := make(chan Event, 32)
    go a.streamEvents(resp.Body, ch)
    return ch, nil
}
```

**Event streaming goroutine:**
```go
func (a *Anthropic) streamEvents(body io.ReadCloser, ch chan<- Event) {
    defer close(ch)
    defer body.Close()

    reader := NewSSEReader(body)
    var toolBuilders []*toolCallBuilder // indexed by content block index

    for {
        sse, err := reader.Next()
        if err == io.EOF {
            return
        }
        if err != nil {
            ch <- Event{Type: EventError, Error: fmt.Errorf("reading SSE: %w", err)}
            return
        }

        switch sse.Type {
        case "message_start":
            // Parse initial usage if present
            var msg struct {
                Message struct {
                    Usage struct {
                        InputTokens int `json:"input_tokens"`
                    } `json:"usage"`
                } `json:"message"`
            }
            json.Unmarshal([]byte(sse.Data), &msg)
            ch <- Event{Type: EventUsage, Usage: &Usage{InputTokens: msg.Message.Usage.InputTokens}}

        case "content_block_start":
            var block struct {
                Index       int    `json:"index"`
                ContentBlock struct {
                    Type string `json:"type"`
                    ID   string `json:"id"`
                    Name string `json:"name"`
                } `json:"content_block"`
            }
            json.Unmarshal([]byte(sse.Data), &block)

            // Grow toolBuilders slice if needed
            for len(toolBuilders) <= block.Index {
                toolBuilders = append(toolBuilders, nil)
            }

            if block.ContentBlock.Type == "tool_use" {
                toolBuilders[block.Index] = &toolCallBuilder{
                    id:   block.ContentBlock.ID,
                    name: block.ContentBlock.Name,
                }
                ch <- Event{
                    Type:     EventToolCallStart,
                    ToolCall: &ToolCall{ID: block.ContentBlock.ID, Name: block.ContentBlock.Name},
                }
            }

        case "content_block_delta":
            var delta struct {
                Index int `json:"index"`
                Delta struct {
                    Type        string `json:"type"`
                    Text        string `json:"text"`
                    PartialJSON string `json:"partial_json"`
                } `json:"delta"`
            }
            json.Unmarshal([]byte(sse.Data), &delta)

            if delta.Delta.Type == "text_delta" {
                ch <- Event{Type: EventTextDelta, Text: delta.Delta.Text}
            } else if delta.Delta.Type == "input_json_delta" {
                if delta.Index < len(toolBuilders) && toolBuilders[delta.Index] != nil {
                    toolBuilders[delta.Index].buf.WriteString(delta.Delta.PartialJSON)
                    ch <- Event{Type: EventToolCallDelta, Text: delta.Delta.PartialJSON}
                }
            }

        case "content_block_stop":
            var stop struct {
                Index int `json:"index"`
            }
            json.Unmarshal([]byte(sse.Data), &stop)

            if stop.Index < len(toolBuilders) && toolBuilders[stop.Index] != nil {
                tb := toolBuilders[stop.Index]
                ch <- Event{
                    Type: EventToolCallEnd,
                    ToolCall: &ToolCall{
                        ID:    tb.id,
                        Name:  tb.name,
                        Input: json.RawMessage(tb.buf.String()),
                    },
                }
            }

        case "message_delta":
            var md struct {
                Usage struct {
                    OutputTokens int `json:"output_tokens"`
                } `json:"usage"`
            }
            json.Unmarshal([]byte(sse.Data), &md)
            ch <- Event{Type: EventUsage, Usage: &Usage{OutputTokens: md.Usage.OutputTokens}}

        case "message_stop":
            ch <- Event{Type: EventDone}
            return

        case "error":
            var errResp struct {
                Error struct {
                    Message string `json:"message"`
                } `json:"error"`
            }
            json.Unmarshal([]byte(sse.Data), &errResp)
            ch <- Event{Type: EventError, Error: fmt.Errorf("anthropic error: %s", errResp.Error.Message)}
            return
        }
    }
}

type toolCallBuilder struct {
    id   string
    name string
    buf  strings.Builder
}
```

**Request body construction (`buildRequestBody`):**

Converts `provider.Request` to Anthropic API format. Key mapping:

- `Request.System` -> top-level `"system"` field
- `Request.Messages` -> `"messages"` array with role-based content blocks
- `Request.Tools` -> `"tools"` array: `{"name": "...", "description": "...", "input_schema": {...}}`
- Always sets `"stream": true`

Message content block mapping:
- `ContentBlock{Type: "text"}` -> `{"type": "text", "text": "..."}`
- `ContentBlock{Type: "tool_use"}` -> `{"type": "tool_use", "id": "...", "name": "...", "input": {...}}`
- `ContentBlock{Type: "tool_result"}` -> `{"type": "tool_result", "tool_use_id": "...", "content": "...", "is_error": bool}`

```go
func (a *Anthropic) buildRequestBody(req *Request) ([]byte, error) {
    type apiMessage struct {
        Role    string        `json:"role"`
        Content []interface{} `json:"content"`
    }

    var messages []apiMessage
    for _, m := range req.Messages {
        am := apiMessage{Role: string(m.Role)}
        for _, cb := range m.Content {
            switch cb.Type {
            case "text":
                am.Content = append(am.Content, map[string]string{"type": "text", "text": cb.Text})
            case "tool_use":
                am.Content = append(am.Content, map[string]interface{}{
                    "type":  "tool_use",
                    "id":    cb.ToolCall.ID,
                    "name":  cb.ToolCall.Name,
                    "input": json.RawMessage(cb.ToolCall.Input),
                })
            case "tool_result":
                am.Content = append(am.Content, map[string]interface{}{
                    "type":        "tool_result",
                    "tool_use_id": cb.ToolResult.ToolCallID,
                    "content":     cb.ToolResult.Content,
                    "is_error":    cb.ToolResult.IsError,
                })
            }
        }
        messages = append(messages, am)
    }

    type apiTool struct {
        Name        string          `json:"name"`
        Description string          `json:"description"`
        InputSchema json.RawMessage `json:"input_schema"`
    }
    var tools []apiTool
    for _, t := range req.Tools {
        tools = append(tools, apiTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
    }

    body := map[string]interface{}{
        "model":      req.Model,
        "max_tokens": req.MaxTokens,
        "stream":     true,
        "messages":   messages,
    }
    if req.System != "" {
        body["system"] = req.System
    }
    if len(tools) > 0 {
        body["tools"] = tools
    }

    return json.Marshal(body)
}
```

**Test strategy (all using httptest.Server):**
- **Text-only response:** Server returns recorded `testdata/anthropic_text_only.sse`. Verify `EventTextDelta` events concatenate to expected text. Verify `EventDone` is final event.
- **Single tool call:** Server returns `testdata/anthropic_tool_call.sse`. Verify `EventToolCallStart`, multiple `EventToolCallDelta`, `EventToolCallEnd` with complete ToolCall.
- **Multiple tool calls:** Server returns `testdata/anthropic_multi_tool.sse`. Verify two complete tool calls with correct IDs and inputs.
- **Error 401:** Server returns HTTP 401. Verify `Stream()` returns error with status and body.
- **Error 429:** Server returns HTTP 429 with `retry-after` header. Verify error message.
- **Error 500:** Server returns HTTP 500. Verify error.
- **Context cancellation:** Cancel context after first few events. Verify goroutine exits and channel closes.
- **Malformed SSE:** Server returns malformed JSON in data. Verify `EventError` is emitted.

**Testdata files to record:**
```
internal/provider/testdata/
    anthropic_text_only.sse       -- simple text response
    anthropic_tool_call.sse       -- text + one tool call (read file)
    anthropic_multi_tool.sse      -- text + two tool calls
```

---

### `internal/provider/openai.go` (~200 lines)

**Purpose:** Implement the Provider interface for the OpenAI Chat Completions API.

**Struct and constructor:**
```go
type OpenAI struct {
    apiKey  string
    baseURL string
    client  *http.Client
}

func NewOpenAI(apiKey, baseURL string) *OpenAI {
    if baseURL == "" {
        baseURL = "https://api.openai.com"
    }
    return &OpenAI{
        apiKey:  apiKey,
        baseURL: strings.TrimRight(baseURL, "/"),
        client: &http.Client{
            // Do NOT set Timeout — kills long-running SSE streams.
            Transport: &http.Transport{
                ResponseHeaderTimeout: 30 * time.Second,
                TLSHandshakeTimeout:   10 * time.Second,
                DialContext: (&net.Dialer{
                    Timeout: 10 * time.Second,
                }).DialContext,
            },
        },
    }
}

func (o *OpenAI) Name() string { return "openai" }
```

**Stream implementation:** Same pattern as Anthropic -- make HTTP request, spawn goroutine to parse SSE events.

**Event parsing goroutine:**
```go
func (o *OpenAI) streamEvents(body io.ReadCloser, ch chan<- Event) {
    defer close(ch)
    defer body.Close()

    reader := NewSSEReader(body)
    toolAccumulators := make(map[int]*toolCallBuilder)

    for {
        sse, err := reader.Next()
        if err == io.EOF {
            return
        }
        if err != nil {
            ch <- Event{Type: EventError, Error: err}
            return
        }

        // OpenAI uses data-only events, no named event types
        if sse.Data == "[DONE]" {
            ch <- Event{Type: EventDone}
            return
        }

        var chunk struct {
            Choices []struct {
                Delta struct {
                    Content   *string `json:"content"`
                    ToolCalls []struct {
                        Index    int    `json:"index"`
                        ID       string `json:"id"`
                        Function struct {
                            Name      string `json:"name"`
                            Arguments string `json:"arguments"`
                        } `json:"function"`
                    } `json:"tool_calls"`
                } `json:"delta"`
                FinishReason *string `json:"finish_reason"`
            } `json:"choices"`
            Usage *struct {
                PromptTokens     int `json:"prompt_tokens"`
                CompletionTokens int `json:"completion_tokens"`
            } `json:"usage"`
        }

        if err := json.Unmarshal([]byte(sse.Data), &chunk); err != nil {
            ch <- Event{Type: EventError, Error: fmt.Errorf("parsing chunk: %w", err)}
            return
        }

        // Handle usage (sent in final chunk with stream_options.include_usage)
        if chunk.Usage != nil {
            ch <- Event{
                Type: EventUsage,
                Usage: &Usage{
                    InputTokens:  chunk.Usage.PromptTokens,
                    OutputTokens: chunk.Usage.CompletionTokens,
                },
            }
        }

        if len(chunk.Choices) == 0 {
            continue
        }

        choice := chunk.Choices[0]

        // Text content
        if choice.Delta.Content != nil && *choice.Delta.Content != "" {
            ch <- Event{Type: EventTextDelta, Text: *choice.Delta.Content}
        }

        // Tool calls
        for _, tc := range choice.Delta.ToolCalls {
            if tc.ID != "" {
                // New tool call starting
                toolAccumulators[tc.Index] = &toolCallBuilder{
                    id:   tc.ID,
                    name: tc.Function.Name,
                }
                ch <- Event{
                    Type:     EventToolCallStart,
                    ToolCall: &ToolCall{ID: tc.ID, Name: tc.Function.Name},
                }
            }
            if tc.Function.Arguments != "" {
                if acc, ok := toolAccumulators[tc.Index]; ok {
                    acc.buf.WriteString(tc.Function.Arguments)
                    ch <- Event{Type: EventToolCallDelta, Text: tc.Function.Arguments}
                }
            }
        }

        // Finish reason
        if choice.FinishReason != nil {
            switch *choice.FinishReason {
            case "tool_calls":
                // Finalize all tool accumulators
                for _, acc := range toolAccumulators {
                    ch <- Event{
                        Type: EventToolCallEnd,
                        ToolCall: &ToolCall{
                            ID:    acc.id,
                            Name:  acc.name,
                            Input: json.RawMessage(acc.buf.String()),
                        },
                    }
                }
            case "stop":
                // Normal end, Done will come from [DONE]
            }
        }
    }
}
```

**Request body construction (`buildRequestBody`):**

Converts `provider.Request` to OpenAI API format. Key differences from Anthropic:

- System prompt is a message with `role: "system"` (not a top-level field).
- Tool results use `role: "tool"` (not a content block within a user message).
- Tool definitions are wrapped in `{"type": "function", "function": {...}}`.
- Tool call content blocks on assistant messages use `"tool_calls"` array.
- Must include `"stream_options": {"include_usage": true}` for token counts.

```go
func (o *OpenAI) buildRequestBody(req *Request) ([]byte, error) {
    var messages []interface{}

    // System message
    if req.System != "" {
        messages = append(messages, map[string]string{"role": "system", "content": req.System})
    }

    for _, m := range req.Messages {
        // Check if this message contains tool results
        hasToolResults := false
        for _, cb := range m.Content {
            if cb.Type == "tool_result" {
                hasToolResults = true
                break
            }
        }

        if hasToolResults {
            // Each tool result is a separate message with role "tool"
            for _, cb := range m.Content {
                if cb.Type == "tool_result" {
                    messages = append(messages, map[string]string{
                        "role":         "tool",
                        "tool_call_id": cb.ToolResult.ToolCallID,
                        "content":      cb.ToolResult.Content,
                    })
                }
            }
            continue
        }

        // Check for tool calls (assistant message)
        hasToolCalls := false
        for _, cb := range m.Content {
            if cb.Type == "tool_use" {
                hasToolCalls = true
                break
            }
        }

        if hasToolCalls {
            var content string
            var toolCalls []map[string]interface{}
            for _, cb := range m.Content {
                if cb.Type == "text" {
                    content = cb.Text
                } else if cb.Type == "tool_use" {
                    toolCalls = append(toolCalls, map[string]interface{}{
                        "id":   cb.ToolCall.ID,
                        "type": "function",
                        "function": map[string]interface{}{
                            "name":      cb.ToolCall.Name,
                            "arguments": string(cb.ToolCall.Input),
                        },
                    })
                }
            }
            msg := map[string]interface{}{
                "role":       string(m.Role),
                "tool_calls": toolCalls,
            }
            if content != "" {
                msg["content"] = content
            }
            messages = append(messages, msg)
            continue
        }

        // Simple text message
        var textParts []string
        for _, cb := range m.Content {
            if cb.Type == "text" {
                textParts = append(textParts, cb.Text)
            }
        }
        messages = append(messages, map[string]string{
            "role":    string(m.Role),
            "content": strings.Join(textParts, "\n"),
        })
    }

    // Tool definitions
    type funcDef struct {
        Name        string          `json:"name"`
        Description string          `json:"description"`
        Parameters  json.RawMessage `json:"parameters"`
    }
    type toolDef struct {
        Type     string  `json:"type"`
        Function funcDef `json:"function"`
    }
    var tools []toolDef
    for _, t := range req.Tools {
        tools = append(tools, toolDef{
            Type:     "function",
            Function: funcDef{Name: t.Name, Description: t.Description, Parameters: t.InputSchema},
        })
    }

    body := map[string]interface{}{
        "model":          req.Model,
        "stream":         true,
        "stream_options": map[string]bool{"include_usage": true},
        "messages":       messages,
    }
    if req.MaxTokens > 0 {
        body["max_tokens"] = req.MaxTokens
    }
    if len(tools) > 0 {
        body["tools"] = tools
    }

    return json.Marshal(body)
}
```

**HTTP headers:**
```
Content-Type: application/json
Authorization: Bearer <key>
```

**Test strategy:** Same pattern as Anthropic. httptest.Server with recorded responses.

**Testdata files:**
```
internal/provider/testdata/
    openai_text_only.sse
    openai_tool_call.sse
    openai_multi_tool.sse
```

---

### `internal/store/migrate.go` (~60 lines)

**Purpose:** Create and update the database schema on startup.

**Implementation:**
```go
package store

import (
    "database/sql"
    "fmt"
)

var migrations = []string{
    // Version 1: initial schema
    `CREATE TABLE IF NOT EXISTS sessions (
        id         TEXT PRIMARY KEY,
        project    TEXT NOT NULL,
        title      TEXT NOT NULL DEFAULT '',
        created_at INTEGER NOT NULL,
        updated_at INTEGER NOT NULL
    );
    CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project);
    CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(updated_at DESC);
    CREATE TABLE IF NOT EXISTS messages (
        id         TEXT PRIMARY KEY,
        session_id TEXT NOT NULL REFERENCES sessions(id),
        role       TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
        content    TEXT NOT NULL,
        metadata   TEXT NOT NULL DEFAULT '{}',
        created_at INTEGER NOT NULL
    );
    CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at);`,
}

// Migrate ensures the database schema is up to date.
// Uses SQLite's PRAGMA user_version for version tracking.
func Migrate(db *sql.DB) error {
    var version int
    if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
        return fmt.Errorf("reading schema version: %w", err)
    }

    for i := version; i < len(migrations); i++ {
        tx, err := db.Begin()
        if err != nil {
            return fmt.Errorf("beginning migration %d: %w", i+1, err)
        }

        if _, err := tx.Exec(migrations[i]); err != nil {
            tx.Rollback()
            return fmt.Errorf("applying migration %d: %w", i+1, err)
        }

        if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
            tx.Rollback()
            return fmt.Errorf("setting version %d: %w", i+1, err)
        }

        if err := tx.Commit(); err != nil {
            return fmt.Errorf("committing migration %d: %w", i+1, err)
        }
    }

    return nil
}
```

**Test strategy:**
- Open in-memory SQLite, run `Migrate(db)` twice. Verify idempotent (no error on second run).
- Verify tables exist after migration by querying `sqlite_master`.
- Verify `PRAGMA user_version` matches `len(migrations)`.

---

### `internal/store/store.go` (~180 lines)

**Purpose:** SQLite implementation of the Store interface.

**Implementation:**
```go
package store

import (
    "context"
    "database/sql"
    "fmt"
    "os"
    "path/filepath"
    "time"

    "github.com/google/uuid"
    _ "modernc.org/sqlite"
)

type SQLiteStore struct {
    db *sql.DB
}

// Open opens or creates the SQLite database at the given path.
func Open(dbPath string) (*SQLiteStore, error) {
    if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
        return nil, fmt.Errorf("creating db directory: %w", err)
    }

    db, err := sql.Open("sqlite", dbPath)
    if err != nil {
        return nil, fmt.Errorf("opening database: %w", err)
    }

    // SQLite is single-writer. Limit to one connection to prevent SQLITE_BUSY.
    db.SetMaxOpenConns(1)

    // Set pragmas for performance and correctness
    for _, pragma := range []string{
        "PRAGMA journal_mode=WAL",
        "PRAGMA busy_timeout=5000",
        "PRAGMA foreign_keys=ON",
        "PRAGMA synchronous=NORMAL",
    } {
        if _, err := db.Exec(pragma); err != nil {
            db.Close()
            return nil, fmt.Errorf("setting pragma: %w", err)
        }
    }

    if err := Migrate(db); err != nil {
        db.Close()
        return nil, fmt.Errorf("running migrations: %w", err)
    }

    return &SQLiteStore{db: db}, nil
}

// OpenMemory opens an in-memory SQLite database (for tests).
func OpenMemory() (*SQLiteStore, error) {
    db, err := sql.Open("sqlite", ":memory:")
    if err != nil {
        return nil, err
    }
    if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
        db.Close()
        return nil, err
    }
    if err := Migrate(db); err != nil {
        db.Close()
        return nil, err
    }
    return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) CreateSession(ctx context.Context, project string) (string, error) {
    id := uuid.New().String()
    now := time.Now().Unix()
    _, err := s.db.ExecContext(ctx,
        "INSERT INTO sessions (id, project, title, created_at, updated_at) VALUES (?, ?, '', ?, ?)",
        id, project, now, now,
    )
    if err != nil {
        return "", fmt.Errorf("creating session: %w", err)
    }
    return id, nil
}

func (s *SQLiteStore) GetSession(ctx context.Context, id string) (*Session, error) {
    var sess Session
    err := s.db.QueryRowContext(ctx,
        "SELECT id, project, title, created_at, updated_at FROM sessions WHERE id = ?", id,
    ).Scan(&sess.ID, &sess.Project, &sess.Title, &sess.CreatedAt, &sess.UpdatedAt)
    if err == sql.ErrNoRows {
        return nil, fmt.Errorf("session not found: %s", id)
    }
    if err != nil {
        return nil, fmt.Errorf("getting session: %w", err)
    }
    return &sess, nil
}

func (s *SQLiteStore) ListSessions(ctx context.Context, project string, limit int) ([]Session, error) {
    rows, err := s.db.QueryContext(ctx,
        "SELECT id, project, title, created_at, updated_at FROM sessions WHERE project = ? ORDER BY updated_at DESC LIMIT ?",
        project, limit,
    )
    if err != nil {
        return nil, fmt.Errorf("listing sessions: %w", err)
    }
    defer rows.Close()

    var sessions []Session
    for rows.Next() {
        var sess Session
        if err := rows.Scan(&sess.ID, &sess.Project, &sess.Title, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
            return nil, fmt.Errorf("scanning session: %w", err)
        }
        sessions = append(sessions, sess)
    }
    return sessions, rows.Err()
}

func (s *SQLiteStore) AppendMessage(ctx context.Context, sessionID string, msg *MessageRecord) error {
    if msg.ID == "" {
        msg.ID = uuid.New().String()
    }
    if msg.CreatedAt == 0 {
        msg.CreatedAt = time.Now().Unix()
    }
    if msg.Metadata == "" {
        msg.Metadata = "{}"
    }

    _, err := s.db.ExecContext(ctx,
        "INSERT INTO messages (id, session_id, role, content, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?)",
        msg.ID, sessionID, msg.Role, msg.Content, msg.Metadata, msg.CreatedAt,
    )
    if err != nil {
        return fmt.Errorf("appending message: %w", err)
    }

    // Update session updated_at
    _, err = s.db.ExecContext(ctx,
        "UPDATE sessions SET updated_at = ? WHERE id = ?",
        msg.CreatedAt, sessionID,
    )
    return err
}

func (s *SQLiteStore) GetMessages(ctx context.Context, sessionID string) ([]MessageRecord, error) {
    rows, err := s.db.QueryContext(ctx,
        "SELECT id, session_id, role, content, metadata, created_at FROM messages WHERE session_id = ? ORDER BY created_at",
        sessionID,
    )
    if err != nil {
        return nil, fmt.Errorf("getting messages: %w", err)
    }
    defer rows.Close()

    var messages []MessageRecord
    for rows.Next() {
        var msg MessageRecord
        if err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &msg.Content, &msg.Metadata, &msg.CreatedAt); err != nil {
            return nil, fmt.Errorf("scanning message: %w", err)
        }
        messages = append(messages, msg)
    }
    return messages, rows.Err()
}

func (s *SQLiteStore) UpdateSessionTitle(ctx context.Context, id, title string) error {
    _, err := s.db.ExecContext(ctx, "UPDATE sessions SET title = ? WHERE id = ?", title, id)
    return err
}

func (s *SQLiteStore) Close() error {
    return s.db.Close()
}
```

**Test strategy:**
- All tests use `OpenMemory()`. No file I/O in tests.
- Test `CreateSession` + `GetSession` roundtrip. Verify all fields.
- Test `AppendMessage` + `GetMessages`. Verify ordering by `created_at`.
- Test `ListSessions` with project filter and limit. Create 5 sessions, request 3, verify order.
- Test `UpdateSessionTitle`. Verify title changes.
- Test `AppendMessage` updates `sessions.updated_at`.
- Test foreign key: `AppendMessage` with invalid `session_id` returns error.
- Test `GetSession` with nonexistent ID returns error.

---

### `internal/tool/tool.go` (~60 lines)

**Purpose:** Define the Tool interface and shared helpers.

**Implementation:** As shown in Interface Definitions above, plus:

```go
// MaxOutputLen is the default truncation limit for tool output (100KB).
const MaxOutputLen = 100 * 1024

// IsBinary checks if data likely contains binary content
// by looking for null bytes in the first 512 bytes.
func IsBinary(data []byte) bool {
    check := data
    if len(check) > 512 {
        check = check[:512]
    }
    for _, b := range check {
        if b == 0 {
            return true
        }
    }
    return false
}

// SkipDir returns true for directories that should be skipped during traversal.
func SkipDir(name string) bool {
    switch name {
    case ".git", "node_modules", "vendor", ".nanocode", "__pycache__", ".venv":
        return true
    }
    return false
}
```

**Test strategy:** Test `ParseInput` with valid JSON, invalid JSON, missing fields. Test `TruncateOutput` at boundary. Test `IsBinary` with text and binary data. Test `SkipDir` with known skip names and allowed names.

---

### `internal/tool/read.go` (~80 lines)

**Purpose:** Read file contents with optional line range.

**Implementation:**
```go
package tool

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "strings"

    "github.com/nanocode/nanocode/internal/provider"
)

type ReadTool struct{}

type readInput struct {
    FilePath string `json:"file_path"`
    Offset   int    `json:"offset"`
    Limit    int    `json:"limit"`
}

func (t *ReadTool) Name() string { return "read" }

func (t *ReadTool) Definition() provider.ToolDef {
    return provider.ToolDef{
        Name:        "read",
        Description: "Read file contents. Returns numbered lines. Use offset/limit for large files.",
        InputSchema: json.RawMessage(`{
            "type": "object",
            "properties": {
                "file_path": {"type": "string", "description": "Absolute path to the file to read"},
                "offset": {"type": "integer", "description": "Start line number (1-indexed, default: 1)"},
                "limit": {"type": "integer", "description": "Maximum number of lines to return (default: all)"}
            },
            "required": ["file_path"]
        }`),
    }
}

func (t *ReadTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    in, err := ParseInput[readInput](input)
    if err != nil {
        return "", fmt.Errorf("parsing input: %w", err)
    }

    data, err := os.ReadFile(in.FilePath)
    if err != nil {
        return "", fmt.Errorf("reading file: %w", err)
    }

    if IsBinary(data) {
        return fmt.Sprintf("Binary file: %s (%d bytes)", in.FilePath, len(data)), nil
    }

    lines := strings.Split(string(data), "\n")

    // Apply offset (1-indexed)
    start := 0
    if in.Offset > 0 {
        start = in.Offset - 1
    }
    if start > len(lines) {
        start = len(lines)
    }

    // Apply limit
    end := len(lines)
    if in.Limit > 0 && start+in.Limit < end {
        end = start + in.Limit
    }

    var buf strings.Builder
    for i := start; i < end; i++ {
        fmt.Fprintf(&buf, "%6d\t%s\n", i+1, lines[i])
    }

    return TruncateOutput(buf.String(), MaxOutputLen), nil
}
```

**Test strategy:**
- Create temp file with known content. Read full. Verify line numbers and content.
- Test `offset=5, limit=3`. Verify exactly 3 lines starting from line 5.
- Test missing file. Verify error message contains file path.
- Test binary file (write bytes with null). Verify "Binary file" message.
- Test empty file. Verify empty output (no crash).
- Test offset beyond file length. Verify empty output.

---

### `internal/tool/write.go` (~80 lines)

**Purpose:** Write content to a file, creating parent directories.

**Implementation:**
```go
package tool

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"

    "github.com/nanocode/nanocode/internal/provider"
)

type WriteTool struct{}

type writeInput struct {
    FilePath string `json:"file_path"`
    Content  string `json:"content"`
}

func (t *WriteTool) Name() string { return "write" }

func (t *WriteTool) Definition() provider.ToolDef {
    return provider.ToolDef{
        Name:        "write",
        Description: "Write content to a file. Creates parent directories if needed. Overwrites existing content.",
        InputSchema: json.RawMessage(`{
            "type": "object",
            "properties": {
                "file_path": {"type": "string", "description": "Absolute path to the file to write"},
                "content": {"type": "string", "description": "Content to write to the file"}
            },
            "required": ["file_path", "content"]
        }`),
    }
}

func (t *WriteTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    in, err := ParseInput[writeInput](input)
    if err != nil {
        return "", fmt.Errorf("parsing input: %w", err)
    }

    dir := filepath.Dir(in.FilePath)
    if err := os.MkdirAll(dir, 0755); err != nil {
        return "", fmt.Errorf("creating directory %s: %w", dir, err)
    }

    // Determine file permissions
    perm := os.FileMode(0644)
    if info, err := os.Stat(in.FilePath); err == nil {
        perm = info.Mode().Perm()
    }

    // Write atomically: temp file + rename
    tmpPath := in.FilePath + ".nanocode.tmp"
    if err := os.WriteFile(tmpPath, []byte(in.Content), perm); err != nil {
        return "", fmt.Errorf("writing temp file: %w", err)
    }
    if err := os.Rename(tmpPath, in.FilePath); err != nil {
        os.Remove(tmpPath) // clean up on failure
        return "", fmt.Errorf("renaming temp file: %w", err)
    }

    return fmt.Sprintf("Wrote %d bytes to %s", len(in.Content), in.FilePath), nil
}
```

**Test strategy:**
- Write to new file in `t.TempDir()`. Verify content and default permissions (0644).
- Overwrite existing file. Verify new content, original permissions preserved.
- Write to deep nested path. Verify directories created.
- Write empty content. Verify file exists with 0 bytes.

---

### `internal/tool/edit.go` (~130 lines)

**Purpose:** String-replacement editing. Find `old_string` in file, replace with `new_string`.

**Implementation:**
```go
package tool

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "github.com/nanocode/nanocode/internal/provider"
)

type EditTool struct{}

type editInput struct {
    FilePath   string `json:"file_path"`
    OldString  string `json:"old_string"`
    NewString  string `json:"new_string"`
    ReplaceAll bool   `json:"replace_all"`
}

func (t *EditTool) Name() string { return "edit" }

func (t *EditTool) Definition() provider.ToolDef {
    return provider.ToolDef{
        Name:        "edit",
        Description: "Edit a file by replacing old_string with new_string. The old_string must match exactly and uniquely (unless replace_all is true).",
        InputSchema: json.RawMessage(`{
            "type": "object",
            "properties": {
                "file_path": {"type": "string", "description": "Absolute path to the file to edit"},
                "old_string": {"type": "string", "description": "Exact string to find in the file"},
                "new_string": {"type": "string", "description": "String to replace it with"},
                "replace_all": {"type": "boolean", "description": "Replace all occurrences (default false)", "default": false}
            },
            "required": ["file_path", "old_string", "new_string"]
        }`),
    }
}

func (t *EditTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    in, err := ParseInput[editInput](input)
    if err != nil {
        return "", fmt.Errorf("parsing input: %w", err)
    }

    data, err := os.ReadFile(in.FilePath)
    if err != nil {
        return "", fmt.Errorf("reading file: %w", err)
    }

    content := string(data)
    count := strings.Count(content, in.OldString)

    if count == 0 {
        return "", fmt.Errorf("old_string not found in %s", in.FilePath)
    }
    if count > 1 && !in.ReplaceAll {
        return "", fmt.Errorf("old_string found %d times in %s; provide more context to make it unique, or set replace_all to true", count, in.FilePath)
    }

    var newContent string
    if in.ReplaceAll {
        newContent = strings.ReplaceAll(content, in.OldString, in.NewString)
    } else {
        newContent = strings.Replace(content, in.OldString, in.NewString, 1)
    }

    // Write atomically
    perm := os.FileMode(0644)
    if info, err := os.Stat(in.FilePath); err == nil {
        perm = info.Mode().Perm()
    }
    tmpPath := in.FilePath + ".nanocode.tmp"
    if err := os.WriteFile(tmpPath, []byte(newContent), perm); err != nil {
        return "", fmt.Errorf("writing: %w", err)
    }
    if err := os.Rename(tmpPath, in.FilePath); err != nil {
        os.Remove(tmpPath)
        return "", fmt.Errorf("renaming: %w", err)
    }

    snippet := diffSnippet(newContent, in.NewString)
    return fmt.Sprintf("Edited %s (%d replacement(s))\n%s", filepath.Base(in.FilePath), count, snippet), nil
}

// diffSnippet shows a few lines of context around the replacement in the new content.
func diffSnippet(newContent, newStr string) string {
    idx := strings.Index(newContent, newStr)
    if idx < 0 {
        return ""
    }

    lines := strings.Split(newContent, "\n")
    // Find which line the change starts on
    charCount := 0
    startLine := 0
    for i, line := range lines {
        if charCount+len(line)+1 > idx {
            startLine = i
            break
        }
        charCount += len(line) + 1
    }

    // Show 3 lines before and after the replacement
    from := startLine - 3
    if from < 0 {
        from = 0
    }
    newLines := strings.Split(newStr, "\n")
    to := startLine + len(newLines) + 3
    if to > len(lines) {
        to = len(lines)
    }

    var buf strings.Builder
    for i := from; i < to; i++ {
        fmt.Fprintf(&buf, " %4d | %s\n", i+1, lines[i])
    }
    return buf.String()
}
```

**Test strategy:**
- Single match: verify replacement and diff snippet.
- Multiple matches, `replace_all=false`: verify error with count.
- Multiple matches, `replace_all=true`: verify all replaced.
- No match: verify error message.
- File not found: verify error.
- Replacement at start/end of file.
- Multi-line old_string and new_string.

---

### `internal/tool/glob.go` (~80 lines)

**Purpose:** Find files matching a glob pattern.

**Implementation:**
```go
package tool

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"

    "github.com/nanocode/nanocode/internal/provider"
)

type GlobTool struct{}

type globInput struct {
    Pattern string `json:"pattern"`
    Path    string `json:"path"`
}

func (t *GlobTool) Name() string { return "glob" }

func (t *GlobTool) Definition() provider.ToolDef {
    return provider.ToolDef{
        Name:        "glob",
        Description: "Find files matching a glob pattern. Supports ** for recursive matching.",
        InputSchema: json.RawMessage(`{
            "type": "object",
            "properties": {
                "pattern": {"type": "string", "description": "Glob pattern (e.g. **/*.go, src/*.ts)"},
                "path": {"type": "string", "description": "Directory to search in (default: current directory)"}
            },
            "required": ["pattern"]
        }`),
    }
}

func (t *GlobTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    in, err := ParseInput[globInput](input)
    if err != nil {
        return "", fmt.Errorf("parsing input: %w", err)
    }

    root := in.Path
    if root == "" {
        root, _ = os.Getwd()
    }

    const maxResults = 200
    var matches []string

    err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
        if err != nil {
            return nil // skip errors
        }
        if d.IsDir() && SkipDir(d.Name()) {
            return filepath.SkipDir
        }
        if d.IsDir() {
            return nil
        }

        rel, _ := filepath.Rel(root, path)
        if matchGlob(in.Pattern, rel) {
            matches = append(matches, rel)
            if len(matches) >= maxResults {
                return fmt.Errorf("limit reached")
            }
        }
        return nil
    })

    sort.Strings(matches)

    if len(matches) == 0 {
        return "No files matched", nil
    }

    result := strings.Join(matches, "\n")
    if len(matches) >= maxResults {
        result += fmt.Sprintf("\n... (truncated at %d results)", maxResults)
    }
    return result, nil
}

// matchGlob matches a path against a pattern that may contain ** (doublestar).
// ** matches any number of directory levels (including zero).
// Supports multiple ** segments (e.g., src/**/internal/**/*.go).
func matchGlob(pattern, path string) bool {
    // If no doublestar, use filepath.Match directly
    if !strings.Contains(pattern, "**") {
        matched, _ := filepath.Match(pattern, path)
        return matched
    }

    // Split on first ** and handle recursively
    parts := strings.SplitN(pattern, "**", 2)
    prefix := strings.TrimRight(parts[0], "/"+string(filepath.Separator))
    suffix := strings.TrimPrefix(parts[1], "/")
    suffix = strings.TrimPrefix(suffix, string(filepath.Separator))

    // Prefix must match the start of the path
    if prefix != "" {
        if !strings.HasPrefix(path, prefix+"/") && path != prefix {
            return false
        }
        // Strip matched prefix from path for recursive matching
        path = strings.TrimPrefix(path, prefix+"/")
    }

    // If no suffix, ** at end matches everything
    if suffix == "" {
        return true
    }

    // Try matching suffix against every possible tail of the path.
    // This handles ** matching zero or more directory levels.
    pathParts := strings.Split(path, "/")
    for i := 0; i <= len(pathParts); i++ {
        tail := strings.Join(pathParts[i:], "/")
        // Recurse: suffix may contain more ** segments
        if matchGlob(suffix, tail) {
            return true
        }
    }
    return false
}
```

**Test strategy:**
- Create temp directory: `a.go`, `b.txt`, `sub/c.go`, `sub/deep/d.go`.
- Test `*.go` matches only `a.go`.
- Test `**/*.go` matches `a.go`, `sub/c.go`, `sub/deep/d.go`.
- Test `sub/*.go` matches only `sub/c.go`.
- Test `**/*.go` with nested dirs matches all `.go` files at any depth.
- Test multiple `**`: `sub/**/deep/**/*.go` matches `sub/x/deep/y/d.go`.
- Test no matches returns "No files matched".
- Test `.git` directory is skipped.
- Test result limit (create 201 files).

---

### `internal/tool/grep.go` (~100 lines)

**Purpose:** Search file contents with regex.

**Implementation:**
```go
package tool

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "regexp"
    "strings"

    "github.com/nanocode/nanocode/internal/provider"
)

type GrepTool struct{}

type grepInput struct {
    Pattern         string `json:"pattern"`
    Path            string `json:"path"`
    Glob            string `json:"glob"`
    CaseInsensitive bool   `json:"case_insensitive"`
}

func (t *GrepTool) Name() string { return "grep" }

func (t *GrepTool) Definition() provider.ToolDef {
    return provider.ToolDef{
        Name:        "grep",
        Description: "Search file contents with a regex pattern. Returns matching lines with file paths and line numbers.",
        InputSchema: json.RawMessage(`{
            "type": "object",
            "properties": {
                "pattern": {"type": "string", "description": "Regex pattern to search for"},
                "path": {"type": "string", "description": "File or directory to search (default: current directory)"},
                "glob": {"type": "string", "description": "Filter files by glob pattern (e.g. *.go)"},
                "case_insensitive": {"type": "boolean", "description": "Case-insensitive search (default: false)"}
            },
            "required": ["pattern"]
        }`),
    }
}

func (t *GrepTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    in, err := ParseInput[grepInput](input)
    if err != nil {
        return "", fmt.Errorf("parsing input: %w", err)
    }

    pat := in.Pattern
    if in.CaseInsensitive {
        pat = "(?i)" + pat
    }
    re, err := regexp.Compile(pat)
    if err != nil {
        return "", fmt.Errorf("invalid regex: %w", err)
    }

    root := in.Path
    if root == "" {
        root, _ = os.Getwd()
    }

    const maxMatches = 100
    var results []string

    // Check if root is a file
    info, err := os.Stat(root)
    if err != nil {
        return "", fmt.Errorf("stat %s: %w", root, err)
    }

    if !info.IsDir() {
        results = searchFile(root, root, re, maxMatches)
    } else {
        filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
            if err != nil || d.IsDir() {
                if d != nil && d.IsDir() && SkipDir(d.Name()) {
                    return filepath.SkipDir
                }
                return nil
            }

            // Apply glob filter
            if in.Glob != "" {
                matched, _ := filepath.Match(in.Glob, d.Name())
                if !matched {
                    return nil
                }
            }

            rel, _ := filepath.Rel(root, path)
            found := searchFile(path, rel, re, maxMatches-len(results))
            results = append(results, found...)

            if len(results) >= maxMatches {
                return fmt.Errorf("limit")
            }
            return nil
        })
    }

    if len(results) == 0 {
        return "No matches found", nil
    }

    output := strings.Join(results, "\n")
    if len(results) >= maxMatches {
        output += fmt.Sprintf("\n... (truncated at %d matches)", maxMatches)
    }
    return output, nil
}

func searchFile(absPath, displayPath string, re *regexp.Regexp, maxResults int) []string {
    f, err := os.Open(absPath)
    if err != nil {
        return nil
    }
    defer f.Close()

    // Binary check
    buf := make([]byte, 512)
    n, _ := f.Read(buf)
    if IsBinary(buf[:n]) {
        return nil
    }
    f.Seek(0, 0)

    var results []string
    scanner := bufio.NewScanner(f)
    lineNum := 0
    for scanner.Scan() && len(results) < maxResults {
        lineNum++
        line := scanner.Text()
        if re.MatchString(line) {
            results = append(results, fmt.Sprintf("%s:%d:%s", displayPath, lineNum, line))
        }
    }
    return results
}
```

**Test strategy:**
- Create temp files with known content.
- Test simple pattern match. Verify `file:line:content` format.
- Test regex pattern (e.g., `func\s+\w+`).
- Test case insensitive search.
- Test glob filter (search `*.go` only).
- Test single file path (not directory).
- Test binary file skip.
- Test no matches returns "No matches found".
- Test result limit.

---

### `internal/tool/bash.go` (~120 lines)

**Purpose:** Execute shell commands with user confirmation.

**Implementation:**
```go
package tool

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "strings"
    "time"

    "github.com/nanocode/nanocode/internal/provider"
)

type BashTool struct {
    // ConfirmFunc is called before executing a command.
    // Return true to allow execution. Default: interactive Y/n prompt on stderr.
    ConfirmFunc func(command string) bool
}

type bashInput struct {
    Command string `json:"command"`
    Timeout int    `json:"timeout"` // seconds
}

func NewBashTool() *BashTool {
    return &BashTool{ConfirmFunc: defaultConfirm}
}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Definition() provider.ToolDef {
    return provider.ToolDef{
        Name:        "bash",
        Description: "Execute a shell command. The user will be asked to confirm before execution.",
        InputSchema: json.RawMessage(`{
            "type": "object",
            "properties": {
                "command": {"type": "string", "description": "The shell command to execute"},
                "timeout": {"type": "integer", "description": "Timeout in seconds (default: 30, max: 300)"}
            },
            "required": ["command"]
        }`),
    }
}

func (t *BashTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    in, err := ParseInput[bashInput](input)
    if err != nil {
        return "", fmt.Errorf("parsing input: %w", err)
    }

    // Confirm with user
    if !t.ConfirmFunc(in.Command) {
        return "Command rejected by user", nil
    }

    // Set timeout
    timeout := 30
    if in.Timeout > 0 {
        timeout = in.Timeout
    }
    if timeout > 300 {
        timeout = 300
    }

    cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
    defer cancel()

    cmd := exec.CommandContext(cmdCtx, "bash", "-c", in.Command)
    cmd.Dir, _ = os.Getwd()

    output, err := cmd.CombinedOutput()
    result := string(output)

    if err != nil {
        if cmdCtx.Err() == context.DeadlineExceeded {
            result += fmt.Sprintf("\n(timed out after %ds)", timeout)
        }
        exitCode := -1
        if exitErr, ok := err.(*exec.ExitError); ok {
            exitCode = exitErr.ExitCode()
        }
        result = fmt.Sprintf("Exit code %d\n%s", exitCode, result)
    }

    return TruncateOutput(result, MaxOutputLen), nil
}

func defaultConfirm(command string) bool {
    fmt.Fprintf(os.Stderr, "\033[33mRun:\033[0m %s \033[2m[Y/n]\033[0m ", command)
    reader := bufio.NewReader(os.Stdin)
    line, _ := reader.ReadString('\n')
    line = strings.TrimSpace(strings.ToLower(line))
    return line == "" || line == "y" || line == "yes"
}
```

**Test strategy:**
- Set `ConfirmFunc` to always-true in tests.
- Test `echo hello`: verify output contains "hello".
- Test non-zero exit: `exit 1`. Verify "Exit code 1" in output.
- Test timeout: `sleep 60` with 1s timeout. Verify timeout message.
- Test confirmation rejection: set `ConfirmFunc` to always-false. Verify "rejected" message.
- Test output truncation: generate >100KB output.

---

### `internal/tool/subagent.go` (~120 lines)

**Purpose:** Spawn a sub-conversation for delegated tasks.

**Implementation:**
```go
package tool

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"

    "github.com/nanocode/nanocode/internal/provider"
)

type contextKey string

const depthKey contextKey = "nanocode_subagent_depth"
const maxDepth = 3

// EngineRunner is the interface the subagent tool uses to run sub-conversations.
// This avoids a circular import between tool and engine packages.
type EngineRunner interface {
    RunSubagent(ctx context.Context, systemPrompt, task string, onEvent func(provider.Event)) error
}

type SubagentTool struct {
    Runner EngineRunner
}

type subagentInput struct {
    Task    string `json:"task"`
    Context string `json:"context"`
}

func (t *SubagentTool) Name() string { return "subagent" }

func (t *SubagentTool) Definition() provider.ToolDef {
    return provider.ToolDef{
        Name:        "subagent",
        Description: "Delegate a task to a sub-agent. The sub-agent runs its own conversation loop with access to all tools. Use for independent sub-tasks.",
        InputSchema: json.RawMessage(`{
            "type": "object",
            "properties": {
                "task": {"type": "string", "description": "Description of the task for the sub-agent"},
                "context": {"type": "string", "description": "Additional context to provide"}
            },
            "required": ["task"]
        }`),
    }
}

func (t *SubagentTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    in, err := ParseInput[subagentInput](input)
    if err != nil {
        return "", fmt.Errorf("parsing input: %w", err)
    }

    // Check recursion depth
    depth := GetDepth(ctx)
    if depth >= maxDepth {
        return "", fmt.Errorf("maximum sub-agent depth (%d) reached", maxDepth)
    }

    // Increment depth for sub-context
    subCtx := context.WithValue(ctx, depthKey, depth+1)

    // Build sub-agent system prompt
    systemPrompt := "You are a sub-agent. Complete the following task concisely and return the result. Do not ask clarifying questions."
    if in.Context != "" {
        systemPrompt += "\n\nContext:\n" + in.Context
    }

    // Collect sub-agent output
    var output strings.Builder
    err = t.Runner.RunSubagent(subCtx, systemPrompt, in.Task, func(ev provider.Event) {
        if ev.Type == provider.EventTextDelta {
            output.WriteString(ev.Text)
        }
    })
    if err != nil {
        return "", fmt.Errorf("sub-agent failed: %w", err)
    }

    return TruncateOutput(output.String(), MaxOutputLen), nil
}

// GetDepth returns the current sub-agent recursion depth from context.
func GetDepth(ctx context.Context) int {
    if v, ok := ctx.Value(depthKey).(int); ok {
        return v
    }
    return 0
}
```

**Note on circular imports:** The subagent tool needs to call into the engine,
but the engine depends on tools. This is resolved via the `EngineRunner` interface
defined in the tool package. The engine implements this interface and injects
itself into the SubagentTool at construction time.

**Test strategy:**
- Create a mock `EngineRunner` that records the system prompt and task, returns canned text.
- Test normal delegation: verify task reaches runner, output returned.
- Test depth limit: set depth=3 in context, verify error.
- Test context with additional context string: verify it appears in system prompt.
- Test runner error: mock returns error, verify propagation.

---

### `internal/engine/tools.go` (~100 lines)

**Purpose:** Tool registry maps names to implementations and provides dispatch.

**Implementation:**
```go
package engine

import (
    "context"
    "fmt"

    "github.com/nanocode/nanocode/internal/provider"
    "github.com/nanocode/nanocode/internal/tool"
)

// ToolRegistry manages the set of available tools.
type ToolRegistry struct {
    tools map[string]tool.Tool
    order []string // preserve registration order for definitions
}

// NewToolRegistry creates a registry with the given tools.
func NewToolRegistry(tools ...tool.Tool) *ToolRegistry {
    r := &ToolRegistry{
        tools: make(map[string]tool.Tool, len(tools)),
    }
    for _, t := range tools {
        r.tools[t.Name()] = t
        r.order = append(r.order, t.Name())
    }
    return r
}

// Get returns a tool by name.
func (r *ToolRegistry) Get(name string) (tool.Tool, bool) {
    t, ok := r.tools[name]
    return t, ok
}

// Definitions returns all tool definitions in registration order.
func (r *ToolRegistry) Definitions() []provider.ToolDef {
    defs := make([]provider.ToolDef, 0, len(r.order))
    for _, name := range r.order {
        defs = append(defs, r.tools[name].Definition())
    }
    return defs
}

// Execute dispatches a tool call and returns the result.
func (r *ToolRegistry) Execute(ctx context.Context, tc *provider.ToolCall) *provider.ToolResult {
    t, ok := r.tools[tc.Name]
    if !ok {
        return &provider.ToolResult{
            ToolCallID: tc.ID,
            Content:    fmt.Sprintf("Unknown tool: %s", tc.Name),
            IsError:    true,
        }
    }

    result, err := t.Execute(ctx, tc.Input)
    if err != nil {
        return &provider.ToolResult{
            ToolCallID: tc.ID,
            Content:    fmt.Sprintf("Tool error: %s", err.Error()),
            IsError:    true,
        }
    }

    return &provider.ToolResult{
        ToolCallID: tc.ID,
        Content:    result,
        IsError:    false,
    }
}
```

**Test strategy:**
- Register 3 mock tools. Verify `Get` returns correct tool. Verify `Get` for unknown returns false.
- Verify `Definitions` returns all 3 in order.
- Test `Execute` with known tool. Verify result.
- Test `Execute` with unknown tool. Verify `IsError: true` and message.
- Test `Execute` when tool returns error. Verify `IsError: true` and error message.

---

### `internal/engine/engine.go` (~200 lines)

**Purpose:** The core agentic conversation loop.

**Implementation:**
```go
package engine

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"
    "time"

    "github.com/nanocode/nanocode/internal/config"
    "github.com/nanocode/nanocode/internal/provider"
    "github.com/nanocode/nanocode/internal/store"
    "github.com/nanocode/nanocode/internal/tool"
)

const maxIterations = 50
const maxContextMessages = 40 // Keep first message + last N to prevent context overflow

const defaultSystem = `You are Nanocode, a coding assistant. You have access to tools for reading, writing, and editing files, running shell commands, searching codebases, and delegating sub-tasks.

When the user gives you a task:
1. Read relevant files to understand the codebase.
2. Plan your approach.
3. Make changes using the edit and write tools.
4. Verify your changes work by running tests or the build.
5. Report what you did.

Be precise. Make minimal changes. Explain your reasoning.`

// Engine is the core conversation loop.
type Engine struct {
    provider provider.Provider
    tools    *ToolRegistry
    store    store.Store
    config   *config.Config
}

// New creates an Engine with the given dependencies.
// It registers all built-in tools.
func New(p provider.Provider, s store.Store, cfg *config.Config) *Engine {
    bashTool := tool.NewBashTool()
    eng := &Engine{
        provider: p,
        store:    s,
        config:   cfg,
    }

    subagentTool := &tool.SubagentTool{Runner: eng}

    eng.tools = NewToolRegistry(
        &tool.ReadTool{},
        &tool.WriteTool{},
        &tool.EditTool{},
        &tool.GlobTool{},
        &tool.GrepTool{},
        bashTool,
        subagentTool,
    )

    return eng
}

// RunSubagent implements tool.EngineRunner for the subagent tool.
func (e *Engine) RunSubagent(ctx context.Context, systemPrompt, task string, onEvent func(provider.Event)) error {
    // Create a temporary config with the sub-agent system prompt
    subCfg := *e.config
    subCfg.System = systemPrompt

    messages := []provider.Message{
        {Role: provider.RoleUser, Content: []provider.ContentBlock{
            {Type: "text", Text: task},
        }},
    }

    return e.loop(ctx, "", messages, &subCfg, onEvent)
}

// Run starts a conversation from the user's initial prompt.
func (e *Engine) Run(ctx context.Context, sessionID string, prompt string, onEvent func(provider.Event)) error {
    // Build initial user message
    userMsg := provider.Message{
        Role: provider.RoleUser,
        Content: []provider.ContentBlock{
            {Type: "text", Text: prompt},
        },
    }

    // Persist user message
    if sessionID != "" {
        contentJSON, _ := json.Marshal(userMsg.Content)
        e.store.AppendMessage(ctx, sessionID, &store.MessageRecord{
            Role:    string(provider.RoleUser),
            Content: string(contentJSON),
        })
    }

    messages := []provider.Message{userMsg}
    return e.loop(ctx, sessionID, messages, e.config, onEvent)
}

// Resume continues an existing session with a new user message.
func (e *Engine) Resume(ctx context.Context, sessionID string, prompt string, onEvent func(provider.Event)) error {
    // Load existing messages
    records, err := e.store.GetMessages(ctx, sessionID)
    if err != nil {
        return fmt.Errorf("loading messages: %w", err)
    }

    var messages []provider.Message
    for _, rec := range records {
        var content []provider.ContentBlock
        json.Unmarshal([]byte(rec.Content), &content)
        messages = append(messages, provider.Message{
            Role:    provider.Role(rec.Role),
            Content: content,
        })
    }

    // Append new user message
    userMsg := provider.Message{
        Role:    provider.RoleUser,
        Content: []provider.ContentBlock{{Type: "text", Text: prompt}},
    }
    contentJSON, _ := json.Marshal(userMsg.Content)
    e.store.AppendMessage(ctx, sessionID, &store.MessageRecord{
        Role:    string(provider.RoleUser),
        Content: string(contentJSON),
    })

    messages = append(messages, userMsg)
    return e.loop(ctx, sessionID, messages, e.config, onEvent)
}

// loop is the core agentic loop.
func (e *Engine) loop(ctx context.Context, sessionID string, messages []provider.Message, cfg *config.Config, onEvent func(provider.Event)) error {
    system := cfg.System
    if system == "" {
        system = defaultSystem
    }

    // Auto-read project context file (nanocode.md) if it exists.
    // This gives the agent a "map" of the project — the highest-leverage
    // harness engineering pattern for improving task quality.
    if projectCtx, err := os.ReadFile(filepath.Join(cfg.ProjectDir, "nanocode.md")); err == nil {
        system += "\n\n# Project Context\n\n" + string(projectCtx)
    }

    // Track per-file edits to detect doom loops (same file edited repeatedly).
    fileEditCounts := make(map[string]int)
    const maxFileEdits = 5

    for i := 0; i < maxIterations; i++ {
        // Check context cancellation
        if ctx.Err() != nil {
            return ctx.Err()
        }

        // Window messages to prevent context overflow.
        // Keep the first message (original user prompt) + last N messages.
        windowed := windowMessages(messages, maxContextMessages)

        // Build provider request
        req := &provider.Request{
            Model:     cfg.Model,
            Messages:  windowed,
            Tools:     e.tools.Definitions(),
            MaxTokens: cfg.MaxTokens,
            System:    system,
        }

        // Stream response
        events, err := e.provider.Stream(ctx, req)
        if err != nil {
            return fmt.Errorf("provider stream (iteration %d): %w", i+1, err)
        }

        // Collect response
        assistantMsg, err := collectResponse(events, onEvent)
        if err != nil {
            return fmt.Errorf("collecting response (iteration %d): %w", i+1, err)
        }

        // Persist assistant message
        if sessionID != "" {
            contentJSON, _ := json.Marshal(assistantMsg.Content)
            e.store.AppendMessage(ctx, sessionID, &store.MessageRecord{
                Role:    string(provider.RoleAssistant),
                Content: string(contentJSON),
            })
        }

        // Extract tool calls
        var toolCalls []*provider.ToolCall
        for _, cb := range assistantMsg.Content {
            if cb.Type == "tool_use" && cb.ToolCall != nil {
                toolCalls = append(toolCalls, cb.ToolCall)
            }
        }

        // If no tool calls, we are done
        if len(toolCalls) == 0 {
            return nil
        }

        // Execute tools with doom loop detection
        var resultBlocks []provider.ContentBlock
        for _, tc := range toolCalls {
            // Track file-mutating tool calls to detect doom loops
            if tc.Name == "edit" || tc.Name == "write" {
                var inp struct{ FilePath string `json:"file_path"` }
                json.Unmarshal(tc.Input, &inp)
                if inp.FilePath != "" {
                    fileEditCounts[inp.FilePath]++
                    if fileEditCounts[inp.FilePath] > maxFileEdits {
                        resultBlocks = append(resultBlocks, provider.ContentBlock{
                            Type: "tool_result",
                            ToolResult: &provider.ToolResult{
                                ToolCallID: tc.ID,
                                Content:    fmt.Sprintf("Doom loop detected: %s has been edited %d times. Stop and report to the user.", inp.FilePath, fileEditCounts[inp.FilePath]),
                                IsError:    true,
                            },
                        })
                        continue
                    }
                }
            }

            result := e.tools.Execute(ctx, tc)
            resultBlocks = append(resultBlocks, provider.ContentBlock{
                Type:       "tool_result",
                ToolResult: result,
            })
        }

        // Build result message
        resultMsg := provider.Message{Role: provider.RoleUser, Content: resultBlocks}

        // Persist tool results
        if sessionID != "" {
            contentJSON, _ := json.Marshal(resultMsg.Content)
            e.store.AppendMessage(ctx, sessionID, &store.MessageRecord{
                Role:    string(provider.RoleUser),
                Content: string(contentJSON),
            })
        }

        // Append and continue loop
        messages = append(messages, *assistantMsg, resultMsg)
    }

    return fmt.Errorf("maximum iterations (%d) reached", maxIterations)
}

// windowMessages prevents context overflow by keeping the first message
// (original user prompt for task context) and the last maxN messages.
// All messages are still persisted to the store — this only affects what
// is sent to the provider.
func windowMessages(msgs []provider.Message, maxN int) []provider.Message {
    if len(msgs) <= maxN {
        return msgs
    }
    // Keep first message + last (maxN-1) messages
    result := make([]provider.Message, 0, maxN)
    result = append(result, msgs[0])
    result = append(result, msgs[len(msgs)-(maxN-1):]...)
    return result
}

// collectResponse drains the event channel and builds the assistant message.
func collectResponse(events <-chan provider.Event, onEvent func(provider.Event)) (*provider.Message, error) {
    var textBuilder strings.Builder
    var toolCalls []*provider.ToolCall

    for ev := range events {
        if onEvent != nil {
            onEvent(ev)
        }

        switch ev.Type {
        case provider.EventTextDelta:
            textBuilder.WriteString(ev.Text)
        case provider.EventToolCallEnd:
            if ev.ToolCall != nil {
                toolCalls = append(toolCalls, ev.ToolCall)
            }
        case provider.EventError:
            return nil, ev.Error
        }
    }

    msg := &provider.Message{Role: provider.RoleAssistant}
    if textBuilder.Len() > 0 {
        msg.Content = append(msg.Content, provider.ContentBlock{
            Type: "text",
            Text: textBuilder.String(),
        })
    }
    for _, tc := range toolCalls {
        msg.Content = append(msg.Content, provider.ContentBlock{
            Type:     "tool_use",
            ToolCall: tc,
        })
    }

    return msg, nil
}
```

**Test strategy (critical -- most important tests in the project):**

```go
// Mock provider for engine tests
type mockProvider struct {
    responses [][]provider.Event // responses[i] is the events for the i-th Stream() call
    callIdx   int
    requests  []*provider.Request // captures requests for assertions
}

func (m *mockProvider) Stream(ctx context.Context, req *provider.Request) (<-chan provider.Event, error) {
    m.requests = append(m.requests, req)
    if m.callIdx >= len(m.responses) {
        return nil, fmt.Errorf("unexpected stream call %d", m.callIdx)
    }
    ch := make(chan provider.Event, len(m.responses[m.callIdx]))
    go func() {
        defer close(ch)
        for _, ev := range m.responses[m.callIdx] {
            select {
            case ch <- ev:
            case <-ctx.Done():
                return
            }
        }
    }()
    m.callIdx++
    return ch, nil
}

func (m *mockProvider) Name() string { return "mock" }
```

**Test cases:**

1. **Text-only response:** Provider returns `[TextDelta("Hello"), Done]`.
   Verify: single iteration, no tool dispatch, message persisted.

2. **Single tool call -> text response:** Provider call 1 returns
   `[TextDelta("Reading"), ToolCallEnd{read, {file_path: "/tmp/test"}}]`.
   Provider call 2 returns `[TextDelta("Done"), Done]`.
   Verify: read tool executed, result sent back, two messages persisted.

3. **Multiple tool calls in one response:** Provider returns
   `[ToolCallEnd{read, ...}, ToolCallEnd{grep, ...}]`.
   Verify: both tools executed, both results sent to provider.

4. **Error tool result:** Register a tool that always errors.
   Verify: `ToolResult{IsError: true}` sent to provider, loop continues.

5. **Max iterations:** Provider always returns a tool call.
   Verify: error after 50 iterations.

6. **Context cancellation:** Cancel context after first iteration.
   Verify: clean exit with `context.Canceled`.

7. **Provider Stream error:** Provider returns error on first call.
   Verify: error propagated to caller.

8. **Resume:** Pre-populate store with messages. Call Resume. Verify
   provider receives all historical messages plus new one.

---

## Data Flow Diagram

```
User: $ nanocode "fix the nil pointer in auth.go"

main.go
  |-- parseArgs()       -> prompt = "fix the nil pointer in auth.go"
  |-- detectProject()   -> projectDir = "/home/user/myproject"
  |-- config.Load()     -> Config{provider:"anthropic", model:"claude-sonnet-4-..."}
  |-- NewAnthropic()    -> provider
  |-- store.Open()      -> SQLiteStore (opens ~/.local/share/nanocode/nanocode.db)
  |-- engine.New()      -> Engine (registers 7 tools)
  |-- engine.Run(ctx, sessionID, prompt, onEvent)
       |
       |-- store.AppendMessage(user: "fix the nil pointer...")
       |
       |-- LOOP ITERATION 1:
       |    |-- provider.Stream(Request{messages, tools, system})
       |    |    |-- HTTP POST https://api.anthropic.com/v1/messages
       |    |    |   Headers: x-api-key, anthropic-version, Content-Type
       |    |    |   Body: {model, system, messages, tools, stream:true}
       |    |    |-- Response: 200 text/event-stream
       |    |    |-- SSEReader parses events
       |    |    |-- Goroutine sends Events to channel
       |    |
       |    |-- collectResponse(events, onEvent):
       |    |    |-- EventTextDelta "Let me read..." -> onEvent -> fmt.Print
       |    |    |-- EventToolCallStart{read}
       |    |    |-- EventToolCallDelta{partial json}
       |    |    |-- EventToolCallEnd{id:"tc_1", name:"read", input:{file_path:"auth.go"}}
       |    |    |-- EventDone
       |    |    |-- return assistantMsg with text + 1 tool call
       |    |
       |    |-- store.AppendMessage(assistant message as JSON)
       |    |
       |    |-- toolCalls = [{id:"tc_1", name:"read", input:{file_path:"auth.go"}}]
       |    |
       |    |-- tools.Execute(ctx, toolCall):
       |    |    |-- registry.Get("read") -> ReadTool
       |    |    |-- ReadTool.Execute(input) -> file contents with line numbers
       |    |    |-- return ToolResult{ToolCallID:"tc_1", Content:"  1\tpackage auth..."}
       |    |
       |    |-- resultMsg = Message{role:user, content:[ToolResult{...}]}
       |    |-- store.AppendMessage(result message)
       |    |-- messages = [userMsg, assistantMsg, resultMsg]
       |    |-- continue loop
       |
       |-- LOOP ITERATION 2:
       |    |-- provider.Stream(messages now include file contents)
       |    |-- LLM returns: TextDelta + ToolCallEnd{edit, {file_path, old, new}}
       |    |-- tools.Execute("edit") -> applies string replacement
       |    |-- continue loop
       |
       |-- LOOP ITERATION 3:
            |-- provider.Stream(messages now include edit result)
            |-- LLM returns: TextDelta "I fixed the nil pointer by..."
            |-- No tool calls -> return nil (success)
            |-- fmt.Println() adds trailing newline
            |-- Exit 0
```

---

## Error Handling Strategy

### Principles

1. **Errors propagate up.** Every function returns `error`. No silent swallowing.
2. **Wrap with context.** Use `fmt.Errorf("doing X: %w", err)` at every layer.
3. **Tool errors go to the LLM.** Failed tools return `ToolResult{IsError: true}`.
   The LLM can decide to retry, try a different approach, or report the error to the user.
4. **Provider errors: retry once.** Network errors (timeout, connection reset)
   get a single retry with 2-second delay. HTTP 4xx errors are not retried
   (except 429 which uses the `retry-after` header).
5. **Fatal errors exit.** Missing API key, DB open failure: log to stderr, `os.Exit(1)`.

### Error Categories

| Category | Source | Handling |
|----------|--------|----------|
| Config errors | Bad JSON, missing file (non-fatal), missing required field | If file missing: use defaults. If bad JSON: print path + error to stderr, exit(1). |
| Auth errors | 401 from provider | Print "API key invalid or missing. Set $ANTHROPIC_API_KEY" to stderr, exit(1). |
| Rate limit | 429 from provider | Parse `retry-after` header. Sleep and retry once. If still 429, propagate error. |
| Network errors | Timeout, DNS, connection reset | Retry once after 2 seconds. If still failing, propagate error with context. |
| Tool errors | File not found, command failed, regex invalid | Return to LLM as `ToolResult{IsError: true, Content: "error message"}`. |
| Store errors | SQLite write/read failure | Propagate to engine loop, then to main. Log and exit. |
| Context cancel | User hits Ctrl+C or SIGTERM | Clean shutdown: HTTP body closed by context, channel drains, DB closed by defer. |

### Signal Handling

```go
// In main.go run():
ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer cancel()
```

This context is passed through the entire call chain:
- `engine.Run(ctx, ...)` checks `ctx.Err()` at top of each loop iteration
- `provider.Stream(ctx, ...)` uses `http.NewRequestWithContext(ctx, ...)`
- `tool.Execute(ctx, ...)` passes context to `exec.CommandContext(ctx, ...)`
- When cancelled, the HTTP response body is closed, the command is killed,
  and the loop exits cleanly with `context.Canceled`.

### Provider Retry Logic (future improvement, simple for Phase 1)

Phase 1 does not implement retry logic in the provider layer. The provider
returns errors directly. Retry can be added in the engine loop:

```go
// In engine.loop(), around the provider.Stream call:
events, err := e.provider.Stream(ctx, req)
if err != nil && isRetryable(err) {
    time.Sleep(2 * time.Second)
    events, err = e.provider.Stream(ctx, req)
}
if err != nil {
    return fmt.Errorf("provider stream: %w", err)
}
```

For Phase 1, this is a stretch goal. The basic error propagation path is sufficient.

---

## Testing Strategy

### Per-Package Summary

| Package | Test File | Strategy | Key Techniques |
|---------|-----------|----------|----------------|
| `config` | `config_test.go` | Unit | `t.TempDir()` for temp JSON files, `t.Setenv` for env vars |
| `provider` | `sse_test.go` | Unit | `strings.NewReader` with SSE byte streams |
| `provider` | `anthropic_test.go` | Unit | `httptest.NewServer` returning recorded SSE fixtures |
| `provider` | `openai_test.go` | Unit | `httptest.NewServer` returning recorded SSE fixtures |
| `store` | `store_test.go` | Unit | `OpenMemory()` in-memory SQLite, no file I/O |
| `tool` | `tool_test.go` | Unit | Test shared helpers |
| `tool` | `read_test.go` | Unit | `t.TempDir()` with test files |
| `tool` | `write_test.go` | Unit | `t.TempDir()`, verify file content and perms |
| `tool` | `edit_test.go` | Unit | `t.TempDir()`, test replacement edge cases |
| `tool` | `glob_test.go` | Unit | `t.TempDir()` with nested directory structure |
| `tool` | `grep_test.go` | Unit | `t.TempDir()` with test files, regex patterns |
| `tool` | `bash_test.go` | Unit | `ConfirmFunc` override, short safe commands |
| `tool` | `subagent_test.go` | Unit | Mock `EngineRunner`, test depth guard |
| `engine` | `tools_test.go` | Unit | Mock `tool.Tool` implementations |
| `engine` | `engine_test.go` | Integration | `mockProvider` + `store.OpenMemory()` |

### Recorded SSE Fixtures

Store real API responses captured during development:

```
internal/provider/testdata/
    anthropic_text_only.sse       -- "Hello, I can help with that."
    anthropic_tool_call.sse       -- text + read tool call
    anthropic_multi_tool.sse      -- text + read + grep tool calls
    openai_text_only.sse          -- simple text response
    openai_tool_call.sse          -- text + function call
    openai_multi_tool.sse         -- text + 2 function calls
```

These files contain the raw SSE byte stream as received from the API.
The test server reads and returns them verbatim.

### Mock Provider for Engine Tests

```go
type mockProvider struct {
    responses [][]provider.Event
    callIdx   int
    requests  []*provider.Request
}

func (m *mockProvider) Stream(ctx context.Context, req *provider.Request) (<-chan provider.Event, error) {
    m.requests = append(m.requests, req)
    if m.callIdx >= len(m.responses) {
        return nil, fmt.Errorf("unexpected stream call %d", m.callIdx)
    }
    events := m.responses[m.callIdx]
    m.callIdx++

    ch := make(chan provider.Event, len(events))
    go func() {
        defer close(ch)
        for _, ev := range events {
            select {
            case ch <- ev:
            case <-ctx.Done():
                return
            }
        }
    }()
    return ch, nil
}

func (m *mockProvider) Name() string { return "mock" }
```

### Running Tests

```bash
go test ./...                                    # all tests
go test -race ./...                              # race detector (critical for channels)
go test -cover ./...                             # coverage report
go test -v ./internal/engine/... -run TestLoop   # specific test
go test -count=1 ./...                           # disable test cache
```

### Coverage Target

- `internal/engine/`: 90%+ (most critical code)
- `internal/provider/`: 85%+ (SSE parsing edge cases)
- `internal/tool/`: 80%+ (each tool thoroughly tested)
- `internal/store/`: 85%+ (all CRUD paths)
- `internal/config/`: 80%+ (load/merge/expand)
- Overall: 80%+

---

## Build and Run Instructions

### Prerequisites

- Go 1.22+ (uses generics from 1.18, range-over-func from 1.22 if desired)
- An Anthropic API key (`ANTHROPIC_API_KEY`) or OpenAI API key (`OPENAI_API_KEY`)

### Initial Setup

```bash
# Create project
mkdir nanocode && cd nanocode

# Initialize Go module
go mod init github.com/nanocode/nanocode

# Create directory structure
mkdir -p internal/{config,provider,engine,tool,store}
mkdir -p internal/provider/testdata

# Add dependencies
go get modernc.org/sqlite
go get github.com/google/uuid

# Verify dependencies
go mod tidy
```

### Build

```bash
# Development build
go build -o nanocode .

# Production build (stripped binary, no debug info)
CGO_ENABLED=0 go build -ldflags="-s -w" -o nanocode .

# Check binary size
ls -lh nanocode
# Expected: ~20-25MB (mostly from modernc.org/sqlite)
```

### Configure

```bash
# Create global config directory
mkdir -p ~/.config/nanocode

# Write minimal config
cat > ~/.config/nanocode/config.json << 'EOF'
{
    "provider": "anthropic",
    "model": "claude-sonnet-4-20250514",
    "apiKey": "$ANTHROPIC_API_KEY",
    "maxTokens": 8192
}
EOF

# Set API key
export ANTHROPIC_API_KEY=sk-ant-api03-...

# (Optional) Project-level config
cat > nanocode.json << 'EOF'
{
    "model": "claude-sonnet-4-20250514",
    "system": "You are a Go expert. Follow Go idioms and conventions."
}
EOF
```

### Run

```bash
# New conversation
./nanocode "fix the nil pointer in auth.go"

# Resume an existing session
./nanocode --session abc12345 "now add tests for the fix"

# List recent sessions
./nanocode --list

# Override model for this run
./nanocode --model claude-opus-4-20250514 "refactor the database layer"

# Use with OpenAI instead
OPENAI_API_KEY=sk-... ./nanocode --model gpt-4o "explain the architecture"
```

### Test

```bash
# Run all tests
go test ./...

# Run with race detector (important for channel-based streaming)
go test -race ./...

# Run with coverage
go test -cover ./...

# Run specific package
go test -v ./internal/engine/...

# Run specific test
go test -v ./internal/engine/... -run TestEngineTextOnly
```

---

## Default System Prompt

```
You are Nanocode, a coding assistant. You have access to tools for reading,
writing, and editing files, running shell commands, searching codebases,
and delegating sub-tasks.

When the user gives you a task:
1. Read relevant files to understand the codebase.
2. Plan your approach.
3. Make changes using the edit and write tools.
4. Verify your changes work by running tests or the build.
5. Report what you did.

Be precise. Make minimal changes. Explain your reasoning.
```

This is the default. It can be overridden via `config.System` in `nanocode.json`
or `~/.config/nanocode/config.json`.

---

## DEPENDENCIES.md

```markdown
# Dependencies

Every external dependency in go.mod must be justified here.
If a dependency cannot be justified, it must be removed.

## Runtime Dependencies

### modernc.org/sqlite
- **Purpose:** Pure-Go SQLite database driver for database/sql
- **Why not alternatives:**
  - mattn/go-sqlite3 requires CGo. CGo breaks the single-binary constraint,
    complicates cross-compilation, and adds build-time C compiler dependency.
  - modernc.org/sqlite is a mechanical translation of SQLite C code to Go.
    Produces a fully static binary with `go build`. No CGo. No C compiler.
- **Tradeoffs:** ~15MB added to binary size. ~2-3x slower than CGo SQLite
  for write-heavy workloads. Acceptable for our workload (dozens of writes
  per session, not thousands).
- **Transitive dependencies:** Several modernc.org packages (libc, mathutil,
  memory, etc.). All pure Go.

### github.com/google/uuid
- **Purpose:** Generate v4 UUIDs for session and message IDs
- **Why not alternatives:**
  - Could use crypto/rand + manual formatting (~20 lines). UUID package is
    1 file, zero transitive dependencies, well-tested, readable.
  - go.uuid and others have unnecessary features and dependencies.
- **Size impact:** Negligible (single file, no transitive deps).

## Standard Library (no justification needed)

- net/http -- HTTP client for provider APIs
- database/sql -- SQLite access via modernc driver
- encoding/json -- JSON marshal/unmarshal for API payloads and config
- os/exec -- Shell command execution for bash tool
- path/filepath -- File path manipulation, glob matching
- regexp -- Regular expression matching for grep tool
- bufio -- Line-based SSE stream reading
- strings, bytes, fmt, io, os, context, time, sync -- fundamentals

## Deferred Dependencies

### mvdan.cc/sh/v3/syntax (Phase 2)
- **Purpose:** Parse shell commands for the permission system
- **Phase:** Phase 2 (MCP + Permissions)
- **Why:** Allow/deny rules need to inspect shell AST, not just string matching.
  e.g., `rm -rf /` must be blocked even if written as `rm -r -f /`.

### charmbracelet/bubbletea (Phase 3)
- **Purpose:** Terminal UI framework for the TUI client
- **Phase:** Phase 3 (Server + Clients)
- **Why:** Building a TUI from scratch is not justified when bubbletea exists.
```

---

## Risks and Considerations

### Risk 1: SSE Parsing Edge Cases
Both providers have subtly different SSE implementations. Anthropic uses named
event types (`event: content_block_delta`). OpenAI uses only `data:` lines
with JSON payloads. Edge cases include: multi-line data fields, empty events,
reconnection events, and unexpected event types.

**Mitigation:** Record real API responses during development and store them as
`testdata/*.sse` fixtures. Write tests for every documented event type. Add
regression tests when new edge cases are discovered.

### Risk 2: Tool Call JSON Accumulation
Both APIs stream tool call arguments as partial JSON fragments. A tool call
with a 10KB code string arrives as dozens of small chunks. The provider must
accumulate these correctly before attempting to parse.

**Mitigation:** Use `strings.Builder` to accumulate fragments. Never parse
partial JSON. Only parse on `content_block_stop` (Anthropic) or
`finish_reason: "tool_calls"` (OpenAI). Test with large inputs that produce
many fragments.

### Risk 3: Context Window Overflow
Long conversations with many tool calls (e.g., reading 20 large files) can
exceed the model's context limit.

**Mitigation:** Phase 1 includes basic message windowing (`windowMessages`):
the first message (original user prompt) and last 40 messages are sent to the
provider. All messages remain persisted in SQLite. Full context compaction
(summarization of dropped messages) is planned for Phase 4.

### Risk 4: Pure-Go SQLite Performance
`modernc.org/sqlite` is approximately 2-3x slower than CGo SQLite for
write-heavy workloads due to the mechanical C-to-Go translation.

**Mitigation:** Our workload is light: typically 10-50 message writes per
session. WAL mode ensures reads never block writes. Performance is not a
concern for Phase 1. If it becomes one, we can add connection pooling or
batch writes.

### Risk 5: Bash Tool Security (Phase 1)
Phase 1 uses a simple Y/n confirmation prompt for all bash commands. There
are no allow/deny lists or shell command parsing. A model could potentially
construct dangerous commands.

**Mitigation:** The user must explicitly approve every command. The Y/n prompt
displays the full command on stderr. Phase 2 adds the `mvdan.cc/sh` parser
for allow/deny pattern matching. Document this limitation in the README.

### Risk 6: Binary Size
`modernc.org/sqlite` adds approximately 15MB to the binary due to the
translated C code.

**Mitigation:** This is acceptable for a developer tool. Use `-ldflags="-s -w"`
to strip debug info (~2MB savings). UPX compression could reduce further if
needed but adds startup latency.

---

## Estimated Complexity

| Component | File(s) | Lines (est.) | Difficulty | Notes |
|-----------|---------|-------------|------------|-------|
| CLI entry + REPL | `main.go` | 110 | Low-Medium | Arg parsing, wiring, interactive loop |
| Config | `config/config.go` | 120 | Low | JSON load, merge, env expand |
| Provider types | `provider/provider.go` | 100 | Low | Types only, no logic |
| SSE parser | `provider/sse.go` | 80 | Medium | Protocol edge cases |
| Anthropic | `provider/anthropic.go` | 220 | Medium-High | SSE event mapping, tool accumulation |
| OpenAI | `provider/openai.go` | 200 | Medium-High | Different format, indexed tool calls |
| Migrations | `store/migrate.go` | 60 | Low | Version-tracked DDL |
| SQLite store | `store/store.go` | 180 | Low-Medium | Standard CRUD |
| Tool interface | `tool/tool.go` | 60 | Low | Interface + helpers |
| Read tool | `tool/read.go` | 80 | Low | File I/O with line numbers |
| Write tool | `tool/write.go` | 80 | Low | Atomic file write |
| Edit tool | `tool/edit.go` | 130 | Medium | String matching, diff snippet |
| Glob tool | `tool/glob.go` | 80 | Medium | Doublestar matching |
| Grep tool | `tool/grep.go` | 100 | Medium | Regex + directory walk |
| Bash tool | `tool/bash.go` | 120 | Medium | Process mgmt, timeout, confirm |
| Subagent tool | `tool/subagent.go` | 120 | Medium | Recursion guard, engine factory |
| Tool registry | `engine/tools.go` | 100 | Low | Map-based dispatch |
| Engine loop | `engine/engine.go` | 250 | High | Core loop + windowing + doom loop detection |
| **Total production** | | **~2,420** | | **Under 3,000 target** |
| **Tests (estimated)** | | **~1,300** | | |
| **Grand total** | | **~3,720** | | |

The 2,420-line estimate leaves ~580 lines of headroom under the 3,000-line constraint,
allowing for unforeseen complexity in SSE parsing, error handling, and edge cases
without requiring architectural changes.

**Changes from initial estimate:** +110 lines for:
- Transport-level timeouts replacing `http.Client.Timeout` (+10)
- `SetMaxOpenConns(1)` for SQLite (+1)
- SSE `CutPrefix` fix for spec compliance (+3)
- Message windowing to prevent context overflow (+20)
- Doom loop detection per file (+15)
- Project context file reading (`nanocode.md`) (+5)
- `diffSnippet` fix to show new content (+3)
- `ProjectDir` field + full `Load()` body (+10)
- Recursive `matchGlob` for multiple `**` patterns (+13)
- Interactive REPL mode in `main.go` (+30)