package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/robertkohahimn/nanocode/internal/benchmark"
	"github.com/robertkohahimn/nanocode/internal/config"
	"github.com/robertkohahimn/nanocode/internal/engine"
	"github.com/robertkohahimn/nanocode/internal/provider"
	"github.com/robertkohahimn/nanocode/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Check for subcommands before normal arg parsing.
	if len(os.Args) > 1 && os.Args[1] == "benchmark" {
		return runBenchmark(ctx, os.Args[2:])
	}
	if len(os.Args) > 1 && os.Args[1] == "failures" {
		return runFailures(ctx, os.Args[2:])
	}

	prompt, sessionID, listMode, strictMode, modelOverride, autoConfirm, logPath := parseArgs(os.Args[1:])
	if autoConfirm {
		fmt.Fprintln(os.Stderr, "⚠️  Auto-confirm enabled: all shell commands will run without confirmation")
	}
	projectDir := detectProject()

	cfg, err := config.Load(projectDir)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if modelOverride != "" {
		cfg.Model = modelOverride
	}
	if strictMode {
		cfg.StrictMode = true
	}

	if logPath != "" {
		if logPath == "stderr" || logPath == "-" {
			cfg.LogWriter = os.Stderr
		} else {
			logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return fmt.Errorf("opening log file: %w", err)
			}
			defer logFile.Close()
			cfg.LogWriter = logFile
		}
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
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}
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
			idPrefix := s.ID
			if len(idPrefix) > 8 {
				idPrefix = idPrefix[:8]
			}
			fmt.Fprintf(os.Stdout, "%s  %s  %s\n", idPrefix, time.Unix(s.UpdatedAt, 0).Format("2006-01-02 15:04"), s.Title)
		}
		return nil
	}

	// Create a shared stdin reader used by both the REPL and bash tool
	// confirmation prompts to avoid conflicts from multiple buffered readers.
	stdinReader := bufio.NewReader(os.Stdin)

	eng := engine.New(prov, st, cfg, stdinReader, autoConfirm)
	defer eng.Close()
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
	if !isTerminal(os.Stdin) {
		return nil // piped input = single-shot mode, don't enter REPL
	}

	// Interactive REPL loop (uses the shared stdinReader)
	for {
		fmt.Fprintf(os.Stderr, "\n\033[36m>\033[0m ")
		line, err := stdinReader.ReadString('\n')
		if err != nil {
			line = strings.TrimSpace(line)
			if errors.Is(err, io.EOF) && line == "" {
				return nil // EOF with no pending input = clean exit
			}
			if !errors.Is(err, io.EOF) {
				return fmt.Errorf("reading stdin: %w", err)
			}
			// EOF with partial line: process it below
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			return nil
		}

		if err := eng.Resume(ctx, sessionID, line, onEvent); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		}
		fmt.Println()
	}
}

func parseArgs(args []string) (prompt, sessionID string, listMode, strictMode bool, modelOverride string, autoConfirm bool, logPath string) {
	var parts []string
	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Support --flag=value syntax by splitting on first =
		if strings.HasPrefix(arg, "--") && strings.Contains(arg, "=") {
			eqIdx := strings.Index(arg, "=")
			flag := arg[:eqIdx]
			val := arg[eqIdx+1:]
			// Expand in-place: replace current arg with flag, insert value after
			expanded := make([]string, 0, len(args)+1)
			expanded = append(expanded, args[:i]...)
			expanded = append(expanded, flag, val)
			expanded = append(expanded, args[i+1:]...)
			args = expanded
			arg = args[i]
		}

		switch arg {
		case "--session":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				sessionID = args[i]
			} else {
				fmt.Fprintln(os.Stderr, "warning: --session requires a value")
			}
		case "--list":
			listMode = true
		case "--strict":
			strictMode = true
		case "--model":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				modelOverride = args[i]
			} else {
				fmt.Fprintln(os.Stderr, "warning: --model requires a value")
			}
		case "--log":
			if i+1 < len(args) && (args[i+1] == "-" || !strings.HasPrefix(args[i+1], "-")) {
				i++
				logPath = args[i]
			} else {
				fmt.Fprintln(os.Stderr, "warning: --log requires a value")
			}
		case "--yes", "-y":
			autoConfirm = true
		default:
			parts = append(parts, args[i])
		}
	}
	prompt = strings.Join(parts, " ")
	return
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

func runBenchmark(ctx context.Context, args []string) error {
	projectDir := detectProject()
	cfg, err := config.Load(projectDir)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
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
		return fmt.Errorf("unknown provider: %s", cfg.Provider)
	}

	dbPath := filepath.Join(xdgDataHome(), "nanocode", "nanocode.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer st.Close()

	factory := func(workDir string) (benchmark.EngineRunner, error) {
		workCfg := *cfg
		workCfg.ProjectDir = workDir
		workCfg.DisableSnapshot = true
		eng := engine.New(prov, st, &workCfg, bufio.NewReader(strings.NewReader("")), true)
		adapter := &benchmarkEngineAdapter{eng: eng, store: st, projectDir: workDir}
		return adapter, nil
	}

	return benchmark.RunCLI(ctx, args, factory, os.Stdout)
}

// benchmarkEngineAdapter adapts the real engine to the benchmark.EngineRunner interface.
type benchmarkEngineAdapter struct {
	eng        *engine.Engine
	store      store.Store
	projectDir string
}

func (a *benchmarkEngineAdapter) Run(ctx context.Context, prompt string) ([]benchmark.ToolCallRecord, error) {
	sessionID, err := a.store.CreateSession(ctx, a.projectDir)
	if err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}
	if err := a.eng.Run(ctx, sessionID, prompt, func(_ provider.Event) {}); err != nil {
		return nil, err
	}
	engineRecords := a.eng.ConsumeToolRecords()
	records := make([]benchmark.ToolCallRecord, len(engineRecords))
	for i, r := range engineRecords {
		records[i] = benchmark.ToolCallRecord{
			Name:       r.Name,
			DurationMs: r.DurationMs,
			IsError:    r.IsError,
		}
	}
	return records, nil
}

func (a *benchmarkEngineAdapter) Close() error {
	a.eng.Close()
	return nil
}

func runFailures(ctx context.Context, args []string) error {
	dbPath := filepath.Join(xdgDataHome(), "nanocode", "nanocode.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer st.Close()

	return engine.RunFailuresCLI(ctx, args, st, os.Stdout)
}

func xdgDataHome() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), ".local", "share")
	}
	return filepath.Join(home, ".local", "share")
}
