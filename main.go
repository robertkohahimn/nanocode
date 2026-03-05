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

	eng := engine.New(prov, st, cfg, stdinReader)
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
			fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		}
		fmt.Println()
	}
}

func parseArgs(args []string) (prompt, sessionID string, listMode bool, modelOverride string) {
	var parts []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--session":
			if i+1 < len(args) {
				i++
				sessionID = args[i]
			} else {
				fmt.Fprintln(os.Stderr, "warning: --session requires a value")
			}
		case "--list":
			listMode = true
		case "--model":
			if i+1 < len(args) {
				i++
				modelOverride = args[i]
			} else {
				fmt.Fprintln(os.Stderr, "warning: --model requires a value")
			}
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

func xdgDataHome() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}
