package internal

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/sahilm/fuzzy"

	"github.com/imthor/session-search/internal/core"
	"github.com/imthor/session-search/internal/providers"
)

// AllProviders returns registered providers (claude + future ones).
func AllProviders() []providers.Provider {
	var ps []providers.Provider
	for _, p := range providers.Registry {
		ps = append(ps, p)
	}
	return ps
}

// ScanAll discovers sessions from all providers.
func ScanAll(extraRoots map[string][]string) ([]core.Session, error) {
	var all []core.Session
	var mu sync.Mutex

	for name, prov := range providers.Registry {
		roots := prov.DefaultRoots()
		if extra, ok := extraRoots[name]; ok && len(extra) > 0 {
			roots = append(roots, extra...)
		}
		sess, err := prov.DiscoverSessions(roots)
		if err != nil {
			// continue with other providers
			continue
		}
		mu.Lock()
		all = append(all, sess...)
		mu.Unlock()
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Start.After(all[j].Start)
	})
	return all, nil
}

// FindCandidateFilesByRG uses ripgrep for extremely fast file discovery of
// sessions containing the query. This is the key to "as fast as ripgrep".
// It returns the list of .jsonl paths that matched.
func FindCandidateFilesByRG(query string, roots []string) ([]string, error) {
	if query == "" || len(roots) == 0 {
		return nil, nil
	}
	if _, err := exec.LookPath("rg"); err != nil {
		return nil, err
	}

	args := []string{"--files-with-matches", "--no-heading", "-l", "-F", "--glob", "*.jsonl", query}
	args = append(args, roots...)

	cmd := exec.Command("rg", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return []string{}, nil // no matches ok
			}
			// real error (2 etc), return it
			return nil, fmt.Errorf("rg error: %s", string(exitErr.Stderr))
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var files []string
	for _, l := range lines {
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}


// FuzzyFilter performs fast fuzzy matching over the blurb + project fields.
// Returns sessions sorted by best match.
func FuzzyFilter(sessions []core.Session, query string) []core.Session {
	if strings.TrimSpace(query) == "" {
		return sessions
	}

	haystack := make([]string, len(sessions))
	for i, s := range sessions {
		haystack[i] = fmt.Sprintf("%s %s %s", s.Project, s.Preview, s.Blurb)
	}

	matches := fuzzy.Find(query, haystack)

	result := make([]core.Session, 0, len(matches))
	for _, m := range matches {
		result = append(result, sessions[m.Index])
	}
	return result
}

// GroupByProject returns sessions grouped by ProjectKey, sorted newest first.
func GroupByProject(sessions []core.Session) map[string][]core.Session {
	groups := make(map[string][]core.Session)
	for _, s := range sessions {
		key := s.ProjectKey
		if key == "" {
			key = s.Project
		}
		groups[key] = append(groups[key], s)
	}

	for k := range groups {
		sort.Slice(groups[k], func(i, j int) bool {
			return groups[k][i].Start.After(groups[k][j].Start)
		})
	}
	return groups
}

// ResolveRoots returns the directories that should be searched for a provider.
func ResolveRoots(providerName string, overrides []string) []string {
	if len(overrides) > 0 {
		return overrides
	}
	if p, ok := providers.Registry[providerName]; ok {
		return p.DefaultRoots()
	}
	return nil
}

// HasRippingFastSearch returns whether ripgrep is available on the system.
func HasRippingFastSearch() bool {
	_, err := exec.LookPath("rg")
	return err == nil
}

// SessionPath returns a nice display path.
func SessionPath(s core.Session) string {
	if s.Path != "" {
		return filepath.Base(s.Path)
	}
	return s.ID
}