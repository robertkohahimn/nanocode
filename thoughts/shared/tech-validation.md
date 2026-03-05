# Nanocode Technology Validation

Generated: 2026-03-05
Validator: General-Purpose Agent (Claude Opus 4.6)

---

## 1. modernc.org/sqlite (Pure-Go SQLite)

**Verdict: Recommended**

- **Version:** v1.46.1 (Feb 18, 2026), BSD-3-Clause, actively maintained
- **Reasoning:** Mature, well-maintained, perfect for cross-compilation. The ~1.5-3x performance penalty vs CGo SQLite is irrelevant for storing conversation sessions (small reads/writes, not OLTP).
- **Risks:**
  - Binary size increase (~20-30MB). Mitigated with `-ldflags="-s -w"`.
  - No C-level APIs (backup, etc.) — only `database/sql` interface. Not needed for this use case.
  - Concurrent write contention — mitigated by `SetMaxOpenConns(1)` + WAL mode.
- **Alternatives considered:**
  - `ncruces/go-sqlite3` v0.30.5 (Jan 2026, MIT) — Wasm-based, pre-v1.0 (less stable)
  - `crawshaw/sqlite` — lower-level, less maintained
  - `mattn/go-sqlite3` — CGo, breaks cross-compilation
- **Action items:**
  - Always set WAL mode + `SetMaxOpenConns(1)`
  - Use `-ldflags="-s -w"` in build
  - Create `DEPENDENCIES.md` entry with justification

---

## 2. Direct HTTP vs AI SDK

**Verdict: Recommended**

- **Reasoning:** Both Anthropic and OpenAI APIs are straightforward HTTP + SSE. The wire protocols are well-documented. Direct HTTP eliminates transitive dependency chains and gives full control over retry logic, error handling, and streaming.
- **~200 lines per provider estimate:** Realistic. SSE parsing is ~80 lines, request building ~40 lines, type definitions ~60 lines, error handling ~20 lines.
- **Risks:**
  - API changes require manual updates (SDKs abstract this). Mitigated: both APIs are stable, changes are additive.
  - Edge cases in SSE parsing (multi-line data, malformed events). Mitigated: use robust parser with proper event boundary handling.
  - Rate limiting / retry logic must be implemented manually. ~30 lines for exponential backoff with jitter.
- **Alternatives considered:**
  - `sashabaranov/go-openai` — adds coupling, transitive deps
  - `liushuangls/go-anthropic` — same concern
  - Vercel AI SDK (what OpenCode uses) — Node ecosystem, heavyweight
- **Action items:**
  - Never set `http.Client.Timeout` on streaming clients
  - Increase `bufio.Scanner` buffer to 512KB
  - Handle `[DONE]` sentinel before JSON parsing (OpenAI)
  - Handle `event:` + `data:` dual-line format (Anthropic)

---

## 3. mvdan.cc/sh/v3/syntax

**Verdict: Recommended**

- **Version:** v3.12.0 (July 2025), BSD-3-Clause, 244 importers
- **Reasoning:** Battle-tested (powers `shfmt`), full POSIX sh + bash AST. Perfect for extracting command names and building allow/deny permission systems. Pure Go, no dependencies.
- **Risks:**
  - Over-engineering risk: for simple command extraction, a regex might suffice. But AST parsing catches pipes, subshells, command substitution — critical for security.
  - Cannot resolve variables at parse time (`$CMD /tmp`). Must deny variable-based command execution.
- **Alternatives considered:**
  - Simple regex splitting — misses pipes, subshells, command substitution. Not secure.
  - `google/shlex` — only tokenizes, no AST
- **Action items:**
  - Walk full AST including `*syntax.CmdSubst`, `*syntax.Subshell`, `*syntax.ProcSubst`
  - Deny `eval`, `bash -c`, `sh -c`, `exec` outright
  - Test with adversarial inputs (nested subshells, variable expansion)

---

## 4. charmbracelet/bubbletea

**Verdict: Recommended** (for Phase 3, not Phase 1)

- **Version:** v1.3.10 (Sept 2025), MIT, 10,999+ dependent projects
- **Reasoning:** Dominant Go TUI framework. Massive ecosystem (lipgloss, bubbles). Well-maintained by Charm. Not needed for Phase 1 (CLI-only), but correct choice for Phase 3 TUI client.
- **Risks:**
  - Dependency weight — pulls in lipgloss, termenv, etc. Acceptable for a TUI binary.
  - Learning curve for Elm architecture (Model-Update-View). Manageable.
- **Alternatives considered:**
  - `rivo/tview` — widget-based, less flexible for custom UIs
  - `gdamore/tcell` — too low-level
  - Raw ANSI — fragile, not worth the effort
- **Action items:**
  - Defer to Phase 3
  - Consider as a separate binary (`nanocode-tui`) or build tag
  - Use lipgloss for styling, bubbles for common components (spinner, text input)

---

## 5. Subprocess Plugin Protocol (JSON over stdin/stdout)

**Verdict: Recommended**

- **Reasoning:** Simple, language-agnostic, battle-tested pattern. Used by LSP (JSON-RPC over stdio), Terraform providers, VS Code extensions. Process isolation provides security boundaries for free.
- **Risks:**
  - Subprocess spawn latency (~5-20ms per invocation). Acceptable for tool calls (not hot path).
  - No streaming from plugins (request-response only). Could add SSE later if needed.
  - Error handling: must handle process crashes, timeouts, malformed JSON.
- **Alternatives considered:**
  - HashiCorp go-plugin — gRPC over stdio, overkill for this use case
  - Go plugin system (`plugin` package) — fragile, platform-limited, version-locked
  - Shared libraries / dynamic linking — complexity, not language-agnostic
- **Action items:**
  - Define clear JSON schema for plugin protocol (request/response types)
  - Set subprocess timeouts (30s default)
  - Capture stderr for error reporting
  - Defer to Phase 4

---

## 6. Overall Architecture Risks

### Single binary constraint
**Verdict: Acceptable**

Pure-Go SQLite + standard library = no CGo needed. `go build` produces one binary. Only risk is binary size (~30-40MB with SQLite + debug info). Use `-ldflags="-s -w"` to reduce.

### 500-line file limit
**Verdict: Acceptable with caveats**

Go tends toward larger files than other languages (test files especially). 500 lines is tight but achievable if:
- Type definitions are in separate `types.go` files
- Tests are in `_test.go` files (don't count toward limit)
- Provider implementations stay focused (SSE parsing + request building only)

**Risk:** Test files will likely exceed 500 lines. Recommend exempting `_test.go` from the limit, or splitting into `foo_test.go` + `foo_integration_test.go`.

### 3,000-line budget for Phase 1
**Verdict: Tight but achievable**

Rough estimate:

| Component | Lines |
|-----------|-------|
| Types/interfaces | ~200 |
| Engine (conversation loop) | ~250 |
| Anthropic provider | ~250 |
| OpenAI provider | ~250 |
| SSE parser (shared) | ~100 |
| 7 tools (avg ~80 each) | ~560 |
| SQLite store | ~200 |
| Config loading | ~100 |
| CLI entry point | ~80 |
| Utility functions | ~100 |
| **Total** | **~2,090** |

This leaves ~900 lines of headroom. The 7 tools are the biggest variable — `bash` and `edit` tools are more complex than `read` or `glob`. If tools average 80 lines, the budget works. If they average 120 lines, it gets tight.

**Recommendation:** Start with 5 tools (bash, read, write, glob, grep). Defer `edit` and `subagent` to Phase 2 — they're the most complex and can ship later without blocking the MVP.

### Missing pieces for Phase 1

1. **Context window management** — no truncation/compaction strategy means conversations will hit token limits. At minimum, need a "last N messages" windowing approach.
2. **Graceful error display** — API errors need user-friendly formatting, not raw JSON.
3. **Signal handling** — Ctrl+C should cancel the current stream, not kill the process.
4. **Token counting** — at least approximate counting for budget awareness.

---

## Summary

| Choice | Verdict | Risk Level |
|--------|---------|------------|
| modernc.org/sqlite | Recommended | Low |
| Direct HTTP (no SDK) | Recommended | Low |
| mvdan.cc/sh/v3/syntax | Recommended | Low |
| charmbracelet/bubbletea | Recommended (Phase 3) | Low |
| Subprocess plugins | Recommended (Phase 4) | Low |
| 500-line file limit | Acceptable | Medium (test files) |
| 3,000-line budget | Tight but achievable | Medium |

**Overall assessment:** The technology choices are sound. All dependencies are mature, well-maintained, and appropriate for the use case. The main risk is the 3,000-line budget being tight if all 7 tools are implemented — consider deferring `edit` and `subagent` to Phase 2.
