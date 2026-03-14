package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ── Request / response types ──────────────────────────────────────────────────

// CloudNote mirrors models.Note from nnn.rocks.
type CloudNote struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Tags      []string  `json:"tags,omitempty"`
	Pinned    bool      `json:"pinned,omitempty"`
}

// CreateNoteRequest is the body sent to POST /notes.
// CreatedAt and UpdatedAt are optional; when provided the server stores them
// as-is so that pre-existing local notes keep their original timestamps.
type CreateNoteRequest struct {
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Tags      []string   `json:"tags"`
	Pinned    bool       `json:"pinned"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// PatchNoteRequest is the body sent to PATCH /notes/{id}.
// Only non-nil fields are written by the server.
// UpdatedAt is passed through so the server stores the exact local edit time.
type PatchNoteRequest struct {
	Title     *string    `json:"title,omitempty"`
	Body      *string    `json:"body,omitempty"`
	Tags      []string   `json:"tags,omitempty"`
	Pinned    *bool      `json:"pinned,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// ── Notes API calls ───────────────────────────────────────────────────────────

// ListNotes calls GET /notes and returns all notes for the authenticated user.
func (c *Client) ListNotes(ctx context.Context, token string) ([]CloudNote, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.base+"/notes", http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	var notes []CloudNote
	if err := c.do(req, &notes); err != nil {
		return nil, fmt.Errorf("notes/list: %w", err)
	}
	return notes, nil
}

// CreateNote calls POST /notes and returns the created note (including its DB id).
func (c *Client) CreateNote(ctx context.Context, token string, r CreateNoteRequest) (CloudNote, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return CloudNote{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/notes", bytes.NewReader(body))
	if err != nil {
		return CloudNote{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	var note CloudNote
	if err := c.do(req, &note); err != nil {
		return CloudNote{}, fmt.Errorf("notes/create: %w", err)
	}
	return note, nil
}

// DeleteNote calls DELETE /notes/{dbID}.
func (c *Client) DeleteNote(ctx context.Context, token, dbID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.base+"/notes/"+dbID, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	if err := c.do(req, nil); err != nil {
		return fmt.Errorf("notes/delete: %w", err)
	}
	return nil
}

// PatchNote calls PATCH /notes/{dbID} and returns the updated note.
func (c *Client) PatchNote(ctx context.Context, token, dbID string, r PatchNoteRequest) (CloudNote, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return CloudNote{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch,
		c.base+"/notes/"+dbID, bytes.NewReader(body))
	if err != nil {
		return CloudNote{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	var note CloudNote
	if err := c.do(req, &note); err != nil {
		return CloudNote{}, fmt.Errorf("notes/patch: %w", err)
	}
	return note, nil
}
