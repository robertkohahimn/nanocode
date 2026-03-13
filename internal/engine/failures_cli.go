package engine

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/robertkohahimn/nanocode/internal/store"
)

// RunFailuresCLI implements the "failures" subcommand.
// Supports: list [--since=7d], show <id>, annotate <id> --type=T --notes="..."
func RunFailuresCLI(ctx context.Context, args []string, st store.Store, w io.Writer) error {
	if len(args) == 0 {
		return failuresUsage(w)
	}

	switch args[0] {
	case "list":
		return failuresList(ctx, args[1:], st, w)
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("usage: failures show <id>")
		}
		return failuresShow(ctx, args[1], st, w)
	case "annotate":
		if len(args) < 2 {
			return fmt.Errorf("usage: failures annotate <id> --type=TYPE --notes=\"...\"")
		}
		return failuresAnnotate(ctx, args[1], args[2:], st, w)
	default:
		failuresUsage(w)
		return fmt.Errorf("unknown failures subcommand: %q", args[0])
	}
}

func failuresUsage(w io.Writer) error {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  nanocode failures list [--since=7d]")
	fmt.Fprintln(w, "  nanocode failures show <id>")
	fmt.Fprintln(w, "  nanocode failures annotate <id> --type=TYPE --notes=\"...\"")
	return nil
}

func failuresList(ctx context.Context, args []string, st store.Store, w io.Writer) error {
	since := time.Now().Add(-7 * 24 * time.Hour).Unix() // default 7 days
	limit := 50

	for _, arg := range args {
		if strings.HasPrefix(arg, "--since=") {
			dur, err := parseDuration(strings.TrimPrefix(arg, "--since="))
			if err != nil {
				return fmt.Errorf("invalid --since value: %w", err)
			}
			since = time.Now().Add(-dur).Unix()
		}
		if strings.HasPrefix(arg, "--limit=") {
			n, err := strconv.Atoi(strings.TrimPrefix(arg, "--limit="))
			if err != nil {
				return fmt.Errorf("invalid --limit value: %w", err)
			}
			limit = n
		}
	}

	records, err := st.ListFailures(ctx, since, limit)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		fmt.Fprintln(w, "No failures found.")
		return nil
	}

	for _, r := range records {
		ts := time.Unix(r.Timestamp, 0).Format("2006-01-02 15:04")
		idShort := r.ID
		if len(idShort) > 8 {
			idShort = idShort[:8]
		}
		fmt.Fprintf(w, "%s  %s  %-15s  iter=%d  %s\n",
			idShort, ts, r.FailureType, r.Iterations, truncate(r.Description, 60))
	}
	return nil
}

func failuresShow(ctx context.Context, id string, st store.Store, w io.Writer) error {
	rec, err := st.GetFailure(ctx, id)
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "ID:           %s\n", rec.ID)
	fmt.Fprintf(w, "Session:      %s\n", rec.SessionID)
	fmt.Fprintf(w, "Timestamp:    %s\n", time.Unix(rec.Timestamp, 0).Format(time.RFC3339))
	fmt.Fprintf(w, "Type:         %s\n", rec.FailureType)
	fmt.Fprintf(w, "Description:  %s\n", rec.Description)
	fmt.Fprintf(w, "Iterations:   %d\n", rec.Iterations)
	fmt.Fprintf(w, "Tools Used:   %s\n", rec.ToolsUsed)
	fmt.Fprintf(w, "Files:        %s\n", rec.FilesTouched)
	if rec.Notes != "" {
		fmt.Fprintf(w, "Notes:        %s\n", rec.Notes)
	}
	return nil
}

func failuresAnnotate(ctx context.Context, id string, args []string, st store.Store, w io.Writer) error {
	var failType, notes string
	for _, arg := range args {
		if strings.HasPrefix(arg, "--type=") {
			failType = strings.TrimPrefix(arg, "--type=")
		}
		if strings.HasPrefix(arg, "--notes=") {
			notes = strings.TrimPrefix(arg, "--notes=")
		}
	}
	if failType == "" {
		return fmt.Errorf("--type is required")
	}

	if err := st.AnnotateFailure(ctx, id, failType, notes); err != nil {
		return err
	}
	fmt.Fprintf(w, "Annotated failure %s\n", id)
	return nil
}

// parseDuration parses durations like "7d", "24h", "30m".
func parseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}
