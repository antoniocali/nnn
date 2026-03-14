package notes

import (
	"strings"
	"time"
)

// Note represents a single note entry.
type Note struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Tags      []string  `json:"tags,omitempty"`
	Pinned    bool      `json:"pinned,omitempty"`

	// DBID is the UUID assigned by the nnn.rocks database after the note has
	// been synced at least once. Empty for notes that have never been synced.
	DBID string `json:"db_id,omitempty"`
}

// FilterNotes returns notes that match the given query (case-insensitive substring).
func FilterNotes(notes []Note, query string) []Note {
	if query == "" {
		return notes
	}
	var result []Note
	q := strings.ToLower(query)
	for _, n := range notes {
		if strings.Contains(strings.ToLower(n.Title), q) ||
			strings.Contains(strings.ToLower(n.Body), q) {
			result = append(result, n)
		}
	}
	return result
}
