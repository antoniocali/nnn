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
	appName    = "nnn"
	dataFile   = "notes.json"
	configFile = "config.json"
)

// Store handles persisting notes to disk.
type Store struct {
	path       string
	configPath string
}

// Config holds user preferences persisted to config.json.
type Config struct {
	Theme           string    `json:"theme,omitempty"`
	LastUpdateCheck time.Time `json:"last_update_check,omitempty"`
	LatestVersion   string    `json:"latest_version,omitempty"`
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
	return &Store{
		path:       filepath.Join(dir, dataFile),
		configPath: filepath.Join(dir, configFile),
	}, nil
}

// LoadConfig reads config.json, returning a zero-value Config if the file
// does not exist yet.
func (s *Store) LoadConfig() (Config, error) {
	data, err := os.ReadFile(s.configPath)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config file: %w", err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parse config file: %w", err)
	}
	return c, nil
}

// SaveConfig writes cfg to config.json, creating it if necessary.
func (s *Store) SaveConfig(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(s.configPath, data, 0o644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	return nil
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
