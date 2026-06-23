package core

import "time"

// Session is the common representation of one AI conversation session.
// Future providers (Codex, Grok, etc.) should produce this.
type Session struct {
	// Provider identifies the source: "claude", "codex", "grok", etc.
	Provider string `json:"provider"`

	// ID is the unique session identifier (usually the filename or uuid in the log).
	ID string `json:"id"`

	// Project is a human-friendly name of the project/folder the session was started in.
	Project string `json:"project"`

	// ProjectKey is the raw grouping key (e.g. sanitized directory name).
	ProjectKey string `json:"project_key"`

	// Start is when the session began.
	Start time.Time `json:"start"`

	// Preview is a short snippet of the first user message or display.
	Preview string `json:"preview"`

	// Path is the absolute path to the backing file (JSONL, etc.).
	Path string `json:"path"`

	// Blurb is a short amount of text used for fuzzy search (first messages).
	// Full content is not stored here to keep memory low.
	Blurb string `json:"-"`
}

// HasText returns whether the session has any searchable text.
func (s Session) HasText() bool {
	return s.Preview != "" || s.Blurb != ""
}