package tui

type ChangeLogEntry struct {
	Version     string   `json:"version,omitempty,default:latest"`
	Description []string `json:"description,omitempty"`
}

// changelogEntries is the flat list of recent changes shown in the What's New
// overlay. Add a new entry at the top of the slice for each release.
var changelogEntries = []ChangeLogEntry{
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
