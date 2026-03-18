package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/antoniocali/nnn/internal/cloud"
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

	// LastSeenVersion is the last version for which the changelog was shown.
	// When the running version differs, the changelog popup is triggered once.
	LastSeenVersion string `json:"last_seen_version,omitempty"`

	// Cloud auth — set by "nnn auth login", cleared by "nnn auth logout".
	Token string `json:"token,omitempty"`
	Email string `json:"email,omitempty"`
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

// Update finds a note by ID, applies edits, saves, and returns the updated note.
func (s *Store) Update(id, title, body string, tags []string) (notes.Note, error) {
	ns, err := s.Load()
	if err != nil {
		return notes.Note{}, err
	}
	var updated notes.Note
	found := false
	for i := range ns {
		if ns[i].ID == id {
			ns[i].Title = title
			ns[i].Body = body
			ns[i].Tags = tags
			ns[i].UpdatedAt = time.Now()
			updated = ns[i]
			found = true
			break
		}
	}
	if !found {
		return notes.Note{}, fmt.Errorf("note %s not found", id)
	}
	return updated, s.Save(ns)
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

// StripAllDBIDs clears the DBID field on every local note and saves.
// Used by --web purge so local notes are no longer associated with any
// cloud record (they will be treated as unsynced on the next sync).
func (s *Store) StripAllDBIDs() error {
	ns, err := s.Load()
	if err != nil {
		return err
	}
	for i := range ns {
		ns[i].DBID = ""
	}
	return s.Save(ns)
}

// PurgeWebResult summarises the outcome of a web purge.
type PurgeWebResult struct {
	Deleted int // number of cloud notes successfully deleted
	Failed  int // number of cloud notes that could not be deleted
}

// PurgeWeb deletes every cloud note for the authenticated user and then strips
// all DBID fields from the local notes so they are no longer linked to any
// cloud record. It is best-effort: if individual deletes fail the error count
// is recorded in the result but the operation continues.
func (s *Store) PurgeWeb(ctx context.Context, token string) (PurgeWebResult, error) {
	client := cloud.New()
	var result PurgeWebResult

	cloudNotes, err := client.ListNotes(ctx, token)
	if err != nil {
		return result, fmt.Errorf("fetch cloud notes: %w", err)
	}

	for _, cn := range cloudNotes {
		if err := client.DeleteNote(ctx, token, cn.ID); err != nil {
			result.Failed++
		} else {
			result.Deleted++
		}
	}

	// Strip DBIDs locally regardless of partial failures — the local notes
	// should no longer reference cloud records that no longer exist.
	if err := s.StripAllDBIDs(); err != nil {
		return result, fmt.Errorf("strip db ids: %w", err)
	}

	return result, nil
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

// SetDBID writes the cloud database UUID into the local note identified by
// localID. If no note with that ID is found the call is a no-op.
func (s *Store) SetDBID(localID, dbID string) error {
	ns, err := s.Load()
	if err != nil {
		return err
	}
	for i := range ns {
		if ns[i].ID == localID {
			ns[i].DBID = dbID
			return s.Save(ns)
		}
	}
	return nil
}

// SaveToken persists a cloud auth token and the associated email to config.json.
func (s *Store) SaveToken(token, email string) error {
	cfg, err := s.LoadConfig()
	if err != nil {
		return err
	}
	cfg.Token = token
	cfg.Email = email
	return s.SaveConfig(cfg)
}

// ClearToken removes the cloud auth token and email from config.json.
func (s *Store) ClearToken() error {
	cfg, err := s.LoadConfig()
	if err != nil {
		return err
	}
	cfg.Token = ""
	cfg.Email = ""
	return s.SaveConfig(cfg)
}

// SyncResult summarises what changed during a sync operation.
type SyncResult struct {
	Uploaded   int // local notes with no DBID that were POSTed to the cloud
	Downloaded int // cloud notes not present locally that were added
	Updated    int // notes where a conflict was resolved by taking the newer side
	Deleted    int // local notes removed because the cloud no longer has them
}

// SyncWithCloud performs a full two-way sync with conflict resolution:
//
//  1. Every local note with no DBID is POSTed to the cloud.
//  2. Every cloud note absent from local is downloaded and added.
//  3. Every note present on both sides: the version with the later updated_at
//     wins. If local is newer the cloud record is PATCHed; if cloud is newer
//     the local record is overwritten.
//  4. Every local note whose DBID is no longer present on the cloud is deleted
//     locally — cloud deletions always take precedence.
//
// The merged list is persisted to disk before returning.
func (s *Store) SyncWithCloud(ctx context.Context, token string) (SyncResult, error) {
	client := cloud.New()
	var result SyncResult

	// ── Step 1: load local notes ─────────────────────────────────────────────
	local, err := s.Load()
	if err != nil {
		return result, fmt.Errorf("load local notes: %w", err)
	}

	// ── Step 2: push unsynced local notes ────────────────────────────────────
	for i := range local {
		if local[i].DBID != "" {
			continue
		}
		tags := local[i].Tags
		if tags == nil {
			tags = []string{}
		}
		createdAt := local[i].CreatedAt
		updatedAt := local[i].UpdatedAt
		created, err := client.CreateNote(ctx, token, cloud.CreateNoteRequest{
			Title:     local[i].Title,
			Body:      local[i].Body,
			Tags:      tags,
			Pinned:    local[i].Pinned,
			CreatedAt: &createdAt,
			UpdatedAt: &updatedAt,
		})
		if err != nil {
			return result, fmt.Errorf("upload note %q: %w", local[i].ID, err)
		}
		local[i].DBID = created.ID
		result.Uploaded++
	}

	// ── Step 3: fetch cloud notes ────────────────────────────────────────────
	cloudNotes, err := client.ListNotes(ctx, token)
	if err != nil {
		return result, fmt.Errorf("fetch cloud notes: %w", err)
	}

	// Index cloud notes by their ID for O(1) lookup.
	cloudByID := make(map[string]cloud.CloudNote, len(cloudNotes))
	for _, cn := range cloudNotes {
		cloudByID[cn.ID] = cn
	}

	// Index local notes by DBID for conflict resolution.
	localIdxByDBID := make(map[string]int, len(local))
	for i, n := range local {
		if n.DBID != "" {
			localIdxByDBID[n.DBID] = i
		}
	}

	// ── Step 4: conflict resolution for notes present on both sides ──────────
	// Also identifies local notes whose DBID has vanished from the cloud.
	keepLocal := make([]bool, len(local))
	for i := range local {
		keepLocal[i] = true
	}

	for i, n := range local {
		if n.DBID == "" {
			continue // already uploaded in step 2, nothing to reconcile
		}
		cn, existsInCloud := cloudByID[n.DBID]
		if !existsInCloud {
			// Cloud deleted this note — follow the cloud.
			keepLocal[i] = false
			result.Deleted++
			continue
		}
		// Both sides have it — last-write wins on updated_at.
		if cn.UpdatedAt.After(n.UpdatedAt) {
			// Cloud is newer: overwrite local fields.
			tags := cn.Tags
			if tags == nil {
				tags = []string{}
			}
			local[i].Title = cn.Title
			local[i].Body = cn.Body
			local[i].Tags = tags
			local[i].Pinned = cn.Pinned
			local[i].UpdatedAt = cn.UpdatedAt
			result.Updated++
		} else if n.UpdatedAt.After(cn.UpdatedAt) {
			// Local is newer: PATCH the cloud record.
			tags := n.Tags
			if tags == nil {
				tags = []string{}
			}
			title := n.Title
			body := n.Body
			pinned := n.Pinned
			updatedAt := n.UpdatedAt
			if _, err := client.PatchNote(ctx, token, n.DBID, cloud.PatchNoteRequest{
				Title:     &title,
				Body:      &body,
				Tags:      tags,
				Pinned:    &pinned,
				UpdatedAt: &updatedAt,
			}); err != nil {
				// Non-fatal: local copy is already correct; cloud will catch up
				// on the next sync.
				_ = err
			} else {
				result.Updated++
			}
		}
		// Equal timestamps: nothing to do.
	}

	// ── Step 5: remove locally deleted cloud notes ───────────────────────────
	merged := local[:0]
	for i, n := range local {
		if keepLocal[i] {
			merged = append(merged, n)
		}
	}
	local = merged

	// Rebuild the DBID index after removals.
	localIdxByDBID = make(map[string]int, len(local))
	for i, n := range local {
		if n.DBID != "" {
			localIdxByDBID[n.DBID] = i
		}
	}

	// ── Step 6: download notes that only exist on the cloud ──────────────────
	for _, cn := range cloudNotes {
		if _, exists := localIdxByDBID[cn.ID]; exists {
			continue
		}
		tags := cn.Tags
		if tags == nil {
			tags = []string{}
		}
		local = append(local, notes.Note{
			ID:        uuid.New().String(),
			DBID:      cn.ID,
			Title:     cn.Title,
			Body:      cn.Body,
			Tags:      tags,
			Pinned:    cn.Pinned,
			CreatedAt: cn.CreatedAt,
			UpdatedAt: cn.UpdatedAt,
		})
		result.Downloaded++
	}

	// ── Step 7: persist ───────────────────────────────────────────────────────
	if err := s.Save(local); err != nil {
		return result, fmt.Errorf("save synced notes: %w", err)
	}

	return result, nil
}
