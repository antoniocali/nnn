package tui

type ChangeLogEntry struct {
	Version     string   `json:"version,omitempty,default:latest"`
	Description []string `json:"description,omitempty"`
}

// changelogEntries is the flat list of recent changes shown in the What's New
// overlay. Add a new entry at the top of the slice for each release.
var changelogEntries = []ChangeLogEntry{
	{Version: "v1.2.1", Description: []string{
		"Double-click a word in the editor to highlight it, then Ctrl+B / Ctrl+U to wrap it in bold or italic",
		"Fixed the editor cursor landing in the wrong spot when clicking on a body line that wraps onto more than one screen row",
		"Bold/italic/heading shortcuts now shown in the editor's status bar",
	}},
	{Version: "v1.2.0", Description: []string{
		"Markdown formatting shortcuts in the editor — Ctrl+B for bold, Ctrl+U for italic, Ctrl+T to cycle a line through H1/H2/H3",
	}},
	{Version: "v1.1.2", Description: []string{
		"Click to position cursor — click anywhere in the editor to move the cursor to that character",
	}},
	{Version: "v1.1.1", Description: []string{
		"Fixed changelog dialog footer floating instead of pinned to the bottom",
		"Changelog now has two modes: What's New shown once on startup for new versions, and full history accessible anytime with V",
	}},
	{Version: "v1.1.0", Description: []string{"What's new overlay - shown once on first launch after an upgrade (press V to reopen)",
		"Up / down arrow navigation inside the note editor",
		"Cursor visible on empty lines while editing",
		"Markdown rendering in the detail view — notes are now rendered with full syntax highlighting"},
	},
}
