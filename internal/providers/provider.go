package providers

import "github.com/imthor/session-search/internal/core"

// Provider discovers and returns sessions for a particular AI tool.
// Implementations should be fast and avoid loading full conversation history
// into memory unless necessary.
type Provider interface {
	// Name returns the provider identifier, e.g. "claude".
	Name() string

	// DefaultRoots returns the default directories to scan for this provider.
	DefaultRoots() []string

	// DiscoverSessions finds sessions under the given roots.
	// It should be efficient (parallel discovery + parsing) and return
	// lightweight Session objects (metadata + short blurb for search).
	DiscoverSessions(roots []string) ([]core.Session, error)
}

// Registry holds all known providers.
var Registry = map[string]Provider{}

// Register adds a provider.
func Register(p Provider) {
	Registry[p.Name()] = p
}
