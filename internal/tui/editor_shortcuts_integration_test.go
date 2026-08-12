package tui

import (
	"testing"

	"github.com/antoniocali/nnn/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
)

// press sends a single key to the model and returns the resulting Model,
// asserting the message type is what Update actually returns for key input.
func press(t *testing.T, m Model, key tea.KeyMsg) Model {
	t.Helper()
	newModel, _ := m.Update(key)
	m2, ok := newModel.(Model)
	if !ok {
		t.Fatalf("Update did not return a tui.Model")
	}
	return m2
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func keyType(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

// TestEditorShortcutsEndToEnd drives the real Model.Update loop with the same
// tea.KeyMsg values a live terminal session would produce — entering the "new
// note" editor, moving to the body field, and exercising the bold/italic/
// heading shortcuts — to confirm the wiring in handleEditKey actually reaches
// toggleWrap/cycleHeading, not just the helpers in isolation.
func TestEditorShortcutsEndToEnd(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := storage.New()
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	m, err := New(store, "amber", "dev")
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	if m.mode != modeList {
		t.Fatalf("initial mode = %v, want modeList", m.mode)
	}

	// "n" -> new note, starts on the title field.
	m = press(t, m, keyRunes("n"))
	if m.mode != modeNew {
		t.Fatalf("mode after 'n' = %v, want modeNew", m.mode)
	}

	// Tab -> move to body field.
	m = press(t, m, keyType(tea.KeyTab))
	if m.editField != 1 {
		t.Fatalf("editField after tab = %d, want 1 (body)", m.editField)
	}

	// Ctrl+B -> empty bold pair, cursor in the middle.
	m = press(t, m, keyType(tea.KeyCtrlB))
	if m.editBody != "****" {
		t.Fatalf("editBody after ctrl+b = %q, want %q", m.editBody, "****")
	}
	if m.editCursorPos != 2 {
		t.Fatalf("editCursorPos after ctrl+b = %d, want 2", m.editCursorPos)
	}

	// Type "bold" in the middle of the pair.
	m = press(t, m, keyRunes("bold"))
	if m.editBody != "**bold**" {
		t.Fatalf("editBody after typing = %q, want %q", m.editBody, "**bold**")
	}

	// Move to end of line, then Ctrl+U -> empty italic pair.
	m = press(t, m, keyType(tea.KeyEnd))
	m = press(t, m, keyType(tea.KeyCtrlU))
	if m.editBody != "**bold**__" {
		t.Fatalf("editBody after ctrl+u = %q, want %q", m.editBody, "**bold**__")
	}
	m = press(t, m, keyRunes("it"))
	if m.editBody != "**bold**_it_" {
		t.Fatalf("editBody after typing italic = %q, want %q", m.editBody, "**bold**_it_")
	}

	// Fresh line, Ctrl+T cycles heading levels. Move past the trailing "_"
	// first — after typing "it" the cursor sits between it and the closing
	// marker, not at the true end of the line.
	m = press(t, m, keyType(tea.KeyEnd))
	m = press(t, m, keyType(tea.KeyEnter))
	m = press(t, m, keyRunes("Heading"))
	m = press(t, m, keyType(tea.KeyCtrlT))
	wantLine2 := "# Heading"
	if got := lastLine(m.editBody); got != wantLine2 {
		t.Fatalf("body second line after 1st ctrl+t = %q, want %q", got, wantLine2)
	}

	m = press(t, m, keyType(tea.KeyCtrlT))
	if got := lastLine(m.editBody); got != "## Heading" {
		t.Fatalf("body second line after 2nd ctrl+t = %q, want %q", got, "## Heading")
	}

	m = press(t, m, keyType(tea.KeyCtrlT))
	if got := lastLine(m.editBody); got != "### Heading" {
		t.Fatalf("body second line after 3rd ctrl+t = %q, want %q", got, "### Heading")
	}

	m = press(t, m, keyType(tea.KeyCtrlT))
	if got := lastLine(m.editBody); got != "Heading" {
		t.Fatalf("body second line after 4th ctrl+t = %q, want %q", got, "Heading")
	}

	// Shortcuts must not touch the title field.
	m.editField = 0
	m.editTitle = ""
	m.editCursorPos = 0
	m = press(t, m, keyType(tea.KeyCtrlB))
	if m.editTitle != "" {
		t.Fatalf("editTitle after ctrl+b in title field = %q, want unchanged empty string", m.editTitle)
	}
}

func lastLine(body string) string {
	lines := []rune(body)
	// find the last '\n'
	last := -1
	for i, r := range lines {
		if r == '\n' {
			last = i
		}
	}
	return string(lines[last+1:])
}
