package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/imthor/session-search/internal"
	"github.com/imthor/session-search/internal/core"
	_ "github.com/imthor/session-search/internal/providers/claude" // auto-register claude provider
	claude "github.com/imthor/session-search/internal/providers/claude"
	"github.com/imthor/session-search/internal/tui"
)

var (
	query      string
	asJSON     bool
	printField string
	limit      int
	locations  []string
	noTUI      bool
)

func main() {
	root := &cobra.Command{
		Use:   "session-search [query]",
		Short: "Lightning fast fuzzy search over your local AI coding sessions",
		Long: `session-search finds old conversations with Claude (and future Codex/Grok) extremely quickly.

It is designed to feel like ripgrep + fd combined with fzf, but for your chat history.

Primary goals:
- Be as fast as ripgrep for text and fd for discovery.
- Excellent CLI for scripting and other tools/skills (--json).
- Beautiful fast TUI with project grouping when used interactively.
`,
		Args: cobra.ArbitraryArgs,
		RunE: run,
	}

	root.Flags().StringVarP(&query, "query", "q", "", "Search query (fuzzy)")
	root.Flags().BoolVar(&asJSON, "json", false, "Output matching sessions as JSON (forces batch mode)")
	root.Flags().StringVar(&printField, "print", "path", `What to emit on selection/batch: "path", "id" (default path)`)
	root.Flags().IntVarP(&limit, "limit", "n", 0, "Maximum results")
	root.Flags().StringSliceVar(&locations, "locations", nil, "Override scan roots (advanced, applies to claude provider)")
	root.Flags().BoolVar(&noTUI, "no-tui", false, "Force batch/CLI mode even on tty")

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	if query == "" && len(args) > 0 {
		query = strings.Join(args, " ")
	}

	// Build provider overrides
	extra := map[string][]string{}
	if len(locations) > 0 {
		extra["claude"] = locations
	}

	forceBatch := asJSON || noTUI || !isInteractive()

	// Fast rg prefilter ONLY for batch query cases (to keep TUI corpus complete for clearing query)
	if forceBatch && query != "" && internal.HasRippingFastSearch() {
		roots := internal.ResolveRoots("claude", locations)
		cand, err := internal.FindCandidateFilesByRG(query, roots)
		if err != nil {
			return fmt.Errorf("ripgrep failed: %w", err)
		}
		if len(cand) > 0 {
			sessions := claude.ParseSessionFiles(cand)
			return runBatch(sessions, query)
		}
		// no matches from rg (no error), fall to full scan (will find 0)
	}

	sessions, err := internal.ScanAll(extra)
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		fmt.Fprintln(os.Stderr, "No sessions found.")
		return nil
	}

	if forceBatch {
		return runBatch(sessions, query)
	}

	// Interactive TUI - always full corpus
	selected, err := tui.RunTUI(sessions, query)
	if err != nil {
		return err
	}
	if selected != nil {
		emit(selected)
	}
	return nil
}

func runBatch(all []core.Session, q string) error {
	filtered := internal.FuzzyFilter(all, q)
	return emitBatch(filtered, q)
}

func runBatchFromSessions(all []core.Session, q string) error {
	filtered := internal.FuzzyFilter(all, q)
	return emitBatch(filtered, q)
}

func emitBatch(filtered []core.Session, q string) error {
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		type out struct {
			ID       string `json:"id"`
			Provider string `json:"provider"`
			Project  string `json:"project"`
			Path     string `json:"path"`
			Preview  string `json:"preview"`
			Start    string `json:"start"`
			Query    string `json:"query,omitempty"`
		}
		res := make([]out, 0, len(filtered))
		for _, s := range filtered {
			res = append(res, out{
				ID: s.ID, Provider: s.Provider, Project: s.Project, Path: s.Path,
				Preview: s.Preview, Start: s.Start.Format(time.RFC3339), Query: q,
			})
		}
		return enc.Encode(res)
	}

	switch printField {
	case "id":
		for _, s := range filtered {
			fmt.Println(s.ID)
		}
	case "path":
		for _, s := range filtered {
			fmt.Println(s.Path)
		}
	default:
		for _, s := range filtered {
			fmt.Printf("%s\t%s\t%s\n", s.ID, s.Project, s.Path)
		}
	}
	return nil
}

func emit(s *core.Session) {
	switch printField {
	case "id":
		fmt.Println(s.ID)
	case "path":
		fmt.Println(s.Path)
	case "json":
		_ = json.NewEncoder(os.Stdout).Encode(s)
	default:
		fmt.Printf("%s\t%s\n", s.ID, s.Path)
	}
}

func isInteractive() bool {
	fi, _ := os.Stdout.Stat()
	return (fi.Mode() & os.ModeCharDevice) != 0
}
