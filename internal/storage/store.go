package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/antoniocali/nnn/internal/notes"
	"github.com/google/uuid"
)

const (
	appName  = "nnn"
	dataFile = "notes.json"
)

// Store handles persisting notes to disk.
type Store struct {
	path string
}

type storeData struct {
	Notes []notes.Note `json:"notes"`
}

// New returns a Store rooted at the OS config dir.
func New() (*Store, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	return &Store{path: filepath.Join(dir, dataFile)}, nil
}

// Load reads all notes from disk.
func (s *Store) Load() ([]notes.Note, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return []notes.Note{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read notes file: %w", err)
	}
	var d storeData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("parse notes file: %w", err)
	}
	// Sort: pinned first, then by UpdatedAt desc
	sort.Slice(d.Notes, func(i, j int) bool {
		if d.Notes[i].Pinned != d.Notes[j].Pinned {
			return d.Notes[i].Pinned
		}
		return d.Notes[i].UpdatedAt.After(d.Notes[j].UpdatedAt)
	})
	return d.Notes, nil
}

// Save persists all notes to disk.
func (s *Store) Save(ns []notes.Note) error {
	d := storeData{Notes: ns}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal notes: %w", err)
	}
	return os.WriteFile(s.path, data, 0o644)
}

// Create adds a new note and saves.
func (s *Store) Create(title, body string, tags []string) (notes.Note, error) {
	ns, err := s.Load()
	if err != nil {
		return notes.Note{}, err
	}
	now := time.Now()
	n := notes.Note{
		ID:        uuid.New().String(),
		Title:     title,
		Body:      body,
		Tags:      tags,
		CreatedAt: now,
		UpdatedAt: now,
	}
	ns = append(ns, n)
	return n, s.Save(ns)
}

// Update finds a note by ID, applies edits, and saves.
func (s *Store) Update(id, title, body string, tags []string) error {
	ns, err := s.Load()
	if err != nil {
		return err
	}
	found := false
	for i := range ns {
		if ns[i].ID == id {
			ns[i].Title = title
			ns[i].Body = body
			ns[i].Tags = tags
			ns[i].UpdatedAt = time.Now()
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("note %s not found", id)
	}
	return s.Save(ns)
}

// Delete removes a note by ID and saves.
func (s *Store) Delete(id string) error {
	ns, err := s.Load()
	if err != nil {
		return err
	}
	filtered := ns[:0]
	for _, n := range ns {
		if n.ID != id {
			filtered = append(filtered, n)
		}
	}
	return s.Save(filtered)
}

// Purge permanently deletes the notes.json file from disk.
// Returns nil if the file did not exist.
func (s *Store) Purge() error {
	err := os.Remove(s.path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("purge notes: %w", err)
	}
	return nil
}

// TogglePin flips the pinned state of a note.
func (s *Store) TogglePin(id string) error {
	ns, err := s.Load()
	if err != nil {
		return err
	}
	for i := range ns {
		if ns[i].ID == id {
			ns[i].Pinned = !ns[i].Pinned
			ns[i].UpdatedAt = time.Now()
			break
		}
	}
	return s.Save(ns)
}

// Path returns the path to the notes file.
func (s *Store) Path() string {
	return s.path
}
