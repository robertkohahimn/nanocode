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
