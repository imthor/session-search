package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/imthor/session-search/internal/core"
	"github.com/imthor/session-search/internal/providers"
)

// Provider implements providers.Provider for Claude Code / desktop.
type Provider struct{}

func init() {
	providers.Register(Provider{})
}

func (Provider) Name() string { return "claude" }

func (Provider) DefaultRoots() []string {
	home := os.Getenv("HOME")
	if home == "" {
		home = "."
	}
	roots := []string{filepath.Join(home, ".claude")}
	if os.Getenv("HOME") != "" {
		roots = append(roots, filepath.Join(home, "Library", "Application Support", "Claude"))
	}
	if extra := os.Getenv("CLAUDE_DATA_DIRS"); extra != "" {
		for _, p := range strings.Split(extra, ":") {
			if p = strings.TrimSpace(p); p != "" {
				roots = append(roots, p)
			}
		}
	}
	return roots
}

// DiscoverSessions implements fast parallel discovery + parsing.
// It only extracts metadata + short blurb, never the entire transcript.
func (p Provider) DiscoverSessions(roots []string) ([]core.Session, error) {
	files, err := findAllJSONL(roots)
	if err != nil {
		return nil, err
	}

	// Limit concurrency to number of CPUs for I/O + CPU work.
	concurrency := runtime.NumCPU()
	if concurrency < 4 {
		concurrency = 4
	}

	g := new(errgroup.Group)
	g.SetLimit(concurrency)

	var mu sync.Mutex
	var sessions []core.Session

	for _, f := range files {
		f := f // capture
		g.Go(func() error {
			s, ok := parseLightSession(f)
			if ok {
				mu.Lock()
				sessions = append(sessions, s)
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Sort newest first
	for i := range sessions {
		if sessions[i].Provider == "" {
			sessions[i].Provider = "claude"
		}
	}
	sortByTimeDesc(sessions)
	return sessions, nil
}

func findAllJSONL(roots []string) ([]string, error) {
	var files []string
	var mu sync.Mutex

	g := new(errgroup.Group)
	g.SetLimit(8) // discovery workers

	for _, root := range roots {
		root := root
		g.Go(func() error {
			err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return nil // skip errors
				}
				if d.IsDir() {
					// Skip heavy irrelevant dirs inside .claude if any
					name := d.Name()
					if name == "node_modules" || name == ".git" {
						return filepath.SkipDir
					}
					return nil
				}
				if strings.HasSuffix(path, ".jsonl") {
					mu.Lock()
					files = append(files, path)
					mu.Unlock()
				}
				return nil
			})
			return err
		})
	}
	return files, g.Wait()
}

func parseLightSession(path string) (core.Session, bool) {
	f, err := os.Open(path)
	if err != nil {
		return core.Session{}, false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Increase buffer significantly for long lines (tool outputs, diffs, large pastes in sessions)
	scanner.Buffer(make([]byte, 0, 128*1024), 4*1024*1024)

	sess := core.Session{
		Provider: "claude",
		Path:     path,
	}

	dir := filepath.Dir(path)
	sess.ProjectKey = filepath.Base(dir)
	if sess.ProjectKey == "" || sess.ProjectKey == "projects" {
		sess.ProjectKey = "unknown"
	}
	sess.Project = prettifyProject(dir)

	var firstUser string
	var blurbParts []string
	var firstTS time.Time

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) < 10 {
			continue
		}

		var ev map[string]any
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}

		if id, ok := getString(ev, "sessionId"); ok && sess.ID == "" {
			sess.ID = id
		}
		if sess.ID == "" {
			base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
			if len(base) > 8 {
				sess.ID = base
			}
		}

		if ts, ok := getString(ev, "timestamp"); ok {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				if firstTS.IsZero() || t.Before(firstTS) {
					firstTS = t
				}
			}
		}

		if cwd, ok := getString(ev, "cwd"); ok && sess.Project == "unknown" {
			sess.Project = shortenPath(cwd)
		}

		typ, _ := getString(ev, "type")

		// Capture user messages for blurb
		if typ == "user" {
			if msg, ok := ev["message"].(map[string]any); ok {
				if c := extractText(msg["content"]); c != "" {
					if firstUser == "" {
						firstUser = truncate(c, 140)
					}
					if len(blurbParts) < 3 {
						blurbParts = append(blurbParts, c)
					}
				}
			}
		}

		// Capture some assistant for context
		if typ == "assistant" && len(blurbParts) < 4 {
			if msg, ok := ev["message"].(map[string]any); ok {
				if c := extractText(msg["content"]); c != "" {
					blurbParts = append(blurbParts, "→ "+truncate(c, 120))
				}
			}
		}

		// last-prompt / display for initial context
		if disp, ok := getString(ev, "display"); ok && firstUser == "" {
			firstUser = truncate(disp, 140)
		}
	}

	if firstTS.IsZero() {
		firstTS = time.Now()
	}
	sess.Start = firstTS

	if firstUser != "" {
		sess.Preview = firstUser
	} else if len(blurbParts) > 0 {
		sess.Preview = truncate(blurbParts[0], 140)
	} else {
		sess.Preview = "(empty session)"
	}

	sess.Blurb = strings.Join(blurbParts, "\n")

	if sess.ID == "" {
		sess.ID = filepath.Base(path)
	}
	if sess.Project == "" || sess.Project == "unknown" {
		sess.Project = sess.ProjectKey
	}

	return sess, true
}

func getString(m map[string]any, k string) (string, bool) {
	v, ok := m[k]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func extractText(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var out []string
		for _, p := range x {
			if m, ok := p.(map[string]any); ok {
				if t, _ := getString(m, "type"); t == "text" {
					if txt, ok := getString(m, "text"); ok {
						out = append(out, txt)
					}
				}
			}
		}
		return strings.Join(out, " ")
	}
	return ""
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func shortenPath(p string) string {
	home := os.Getenv("HOME")
	if home != "" && strings.HasPrefix(p, home) {
		p = "~" + strings.TrimPrefix(p, home)
	}
	parts := strings.Split(p, "/")
	if len(parts) > 3 {
		return "…/" + strings.Join(parts[len(parts)-3:], "/")
	}
	return p
}

func prettifyProject(dir string) string {
	base := filepath.Base(dir)
	if !strings.HasPrefix(base, "-") {
		return base
	}
	s := strings.TrimPrefix(base, "-")
	parts := strings.Split(s, "-")
	var nice []string
	for i, p := range parts {
		if i == 0 && p == "Users" {
			nice = append(nice, "~")
			continue
		}
		if i == 1 && len(nice) > 0 && nice[0] == "~" {
			continue // skip username
		}
		nice = append(nice, p)
	}
	if len(nice) == 0 {
		return base
	}
	return strings.Join(nice, "/")
}

func sortByTimeDesc(s []core.Session) {
	sort.Slice(s, func(i, j int) bool {
		return s[i].Start.After(s[j].Start)
	})
}

// ParseSessionFiles is exported for the fast rg-candidate path in the CLI.
// It parses only the provided .jsonl paths using the light session parser.
func ParseSessionFiles(paths []string) []core.Session {
	if len(paths) == 0 {
		return nil
	}
	concurrency := runtime.NumCPU()
	if concurrency < 2 {
		concurrency = 2
	}
	g := new(errgroup.Group)
	g.SetLimit(concurrency)

	var mu sync.Mutex
	var out []core.Session

	for _, p := range paths {
		p := p
		g.Go(func() error {
			if s, ok := parseLightSession(p); ok {
				mu.Lock()
				out = append(out, s)
				mu.Unlock()
			}
			return nil
		})
	}
	_ = g.Wait()
	sortByTimeDesc(out)
	return out
}
