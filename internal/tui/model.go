package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/antoniocali/nnn/internal/cloud"
	"github.com/antoniocali/nnn/internal/notes"
	"github.com/antoniocali/nnn/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// ── Modes ────────────────────────────────────────────────────────────────────

type mode int

const (
	modeList      mode = iota // navigating the list
	modeDetail                // reading detail (right panel focused)
	modeEdit                  // editing an existing note
	modeNew                   // creating a new note
	modeSearch                // typing a search query
	modeDelete                // confirm delete
	modeHelp                  // help overlay
	modeChangelog             // what's new overlay
)

// ── Messages ─────────────────────────────────────────────────────────────────

type errMsg struct{ err error }
type savedMsg struct{}
type statusClearMsg struct{}
type saveConfigMsg struct{ theme string }
type saveChangelogSeenMsg struct{ version string }
type cloudErrMsg struct{ text string }
type syncDoneMsg struct {
	result storage.SyncResult
	err    error
}

// ── Model ─────────────────────────────────────────────────────────────────────

type Model struct {
	store *storage.Store

	// data
	allNotes      []notes.Note // unfiltered
	filteredNotes []notes.Note // after search
	cursor        int          // index in filteredNotes

	// layout
	width  int
	height int

	// mode
	mode mode

	// editor fields
	editID         string
	editTitle      string
	editBody       string
	editTags       string // comma-separated, e.g. "work, ideas"
	editField      int    // 0 = title, 1 = body, 2 = tags
	editCursorPos  int    // cursor within current field
	editBodyOffset int    // scroll offset for body lines

	// search
	searchQuery string

	// scroll for detail view
	detailOffset int

	// scroll for help overlay
	helpOffset int

	// changelog overlay
	changelogOffset int
	showChangelog   bool // true when this run is the first with a new version

	// status bar
	statusMsg    string
	statusIsErr  bool
	statusTicker int

	// theme
	theme      Theme
	themeIndex int // index into AllThemes for cycling

	// version string (injected at build time via ldflags)
	version string

	// cloud auth — non-empty when the user is logged in to nnn.rocks
	email string
	token string
}

// New creates a fresh TUI model using the given theme name and version string.
func New(store *storage.Store, themeName string, version string) (Model, error) {
	ns, err := store.Load()
	if err != nil {
		return Model{}, err
	}

	// Resolve theme index (defaults to 0 = amber)
	idx := 0
	for i, t := range AllThemes {
		if t.Name == themeName {
			idx = i
			break
		}
	}

	// Read the stored cloud email (empty if not logged in).
	email := ""
	token := ""
	showChangelog := false
	if cfg, err := store.LoadConfig(); err == nil {
		email = cfg.Email
		token = cfg.Token
		// Show the changelog overlay once when the running version is new.
		// We compare normalized versions (strip leading "v") so "v0.2.0" and
		// "0.2.0" are treated as the same. Dev builds are skipped entirely.
		runningVer := strings.TrimPrefix(version, "v")
		seenVer := strings.TrimPrefix(cfg.LastSeenVersion, "v")
		if runningVer != "" && runningVer != "dev" && runningVer != seenVer {
			showChangelog = true
		}
	}

	// When logged in, fetch the cloud theme and let it override the local value.
	// We do this synchronously here so the TUI starts with the correct theme.
	// A short timeout ensures a slow/unavailable network doesn't block startup.
	if token != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c := cloud.New()
		if cloudCfg, err := c.GetConfig(ctx, token); err == nil && cloudCfg.Theme != "" {
			if cloudCfg.Theme != themeName {
				themeName = cloudCfg.Theme
				// Persist back to local config so the next offline start picks it up.
				if localCfg, err := store.LoadConfig(); err == nil {
					localCfg.Theme = themeName
					_ = store.SaveConfig(localCfg)
				}
			}
		}
	}

	// Re-resolve theme index after possible cloud override.
	idx = 0
	for i, t := range AllThemes {
		if t.Name == themeName {
			idx = i
			break
		}
	}

	m := Model{
		store:         store,
		allNotes:      ns,
		filteredNotes: ns,
		theme:         AllThemes[idx],
		themeIndex:    idx,
		version:       version,
		email:         email,
		token:         token,
		showChangelog: showChangelog,
	}
	if showChangelog {
		m.mode = modeChangelog
	}
	return m, nil
}

func (m Model) Init() tea.Cmd {
	if m.token == "" {
		return nil
	}
	return m.cmdCloudSync()
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case errMsg:
		m.statusMsg = "Error: " + msg.err.Error()
		m.statusIsErr = true
		return m, clearStatusAfter(4 * time.Second)

	case cloudErrMsg:
		m.statusMsg = msg.text
		m.statusIsErr = true
		return m, clearStatusAfter(6 * time.Second)

	case savedMsg:
		m.statusMsg = "Note saved"
		m.statusIsErr = false
		return m, clearStatusAfter(2 * time.Second)

	case statusClearMsg:
		m.statusMsg = ""
		return m, nil

	case saveConfigMsg:
		if cfg, err := m.store.LoadConfig(); err == nil {
			cfg.Theme = msg.theme
			_ = m.store.SaveConfig(cfg)
		}
		return m, nil

	case saveChangelogSeenMsg:
		if cfg, err := m.store.LoadConfig(); err == nil {
			cfg.LastSeenVersion = msg.version
			_ = m.store.SaveConfig(cfg)
		}
		return m, nil

	case syncDoneMsg:
		if msg.err != nil {
			m.statusMsg = cloud.ClassifyError(msg.err)
			m.statusIsErr = true
			return m, clearStatusAfter(6 * time.Second)
		}
		// Reload notes to pick up any downloaded/updated notes.
		ns, err := m.store.Load()
		if err != nil {
			return m, sendErr(err)
		}
		m.allNotes = ns
		if m.searchQuery != "" {
			m.filteredNotes = notes.FilterNotes(ns, m.searchQuery)
		} else {
			m.filteredNotes = ns
		}
		if m.cursor >= len(m.filteredNotes) && len(m.filteredNotes) > 0 {
			m.cursor = len(m.filteredNotes) - 1
		}
		r := msg.result
		if r.Uploaded+r.Downloaded+r.Updated+r.Deleted > 0 {
			m.statusMsg = fmt.Sprintf("Synced: +%d ↑%d ~%d -%d",
				r.Downloaded, r.Uploaded, r.Updated, r.Deleted)
			m.statusIsErr = false
			return m, clearStatusAfter(4 * time.Second)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeList:
		return m.handleListKey(msg)
	case modeDetail:
		return m.handleDetailKey(msg)
	case modeEdit, modeNew:
		return m.handleEditKey(msg)
	case modeSearch:
		return m.handleSearchKey(msg)
	case modeDelete:
		return m.handleDeleteKey(msg)
	case modeHelp:
		return m.handleHelpKey(msg)
	case modeChangelog:
		return m.handleChangelogKey(msg)
	}
	return m, nil
}

// ── List mode keys ───────────────────────────────────────────────────────────

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "?":
		m.mode = modeHelp
		return m, nil

	case "j", "down":
		if m.cursor < len(m.filteredNotes)-1 {
			m.cursor++
			m.detailOffset = 0
		}

	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			m.detailOffset = 0
		}

	case "g", "home":
		m.cursor = 0
		m.detailOffset = 0

	case "G", "end":
		if len(m.filteredNotes) > 0 {
			m.cursor = len(m.filteredNotes) - 1
		}
		m.detailOffset = 0

	case "enter", "l", "right":
		if len(m.filteredNotes) > 0 {
			m.mode = modeDetail
		}

	case "n":
		m.mode = modeNew
		m.editID = ""
		m.editTitle = ""
		m.editBody = ""
		m.editTags = ""
		m.editField = 0
		m.editCursorPos = 0
		m.editBodyOffset = 0

	case "e":
		if len(m.filteredNotes) > 0 {
			n := m.filteredNotes[m.cursor]
			m.mode = modeEdit
			m.editID = n.ID
			m.editTitle = n.Title
			m.editBody = n.Body
			m.editTags = strings.Join(n.Tags, ", ")
			m.editField = 1
			m.editCursorPos = utf8.RuneCountInString(n.Body)
			m.editBodyOffset = 0
		}

	case "d", "delete":
		if len(m.filteredNotes) > 0 {
			m.mode = modeDelete
		}

	case "p":
		if len(m.filteredNotes) > 0 {
			n := m.filteredNotes[m.cursor]
			if err := m.store.TogglePin(n.ID); err != nil {
				return m, sendErr(err)
			}
			// Fire-and-forget cloud pin sync if logged in and note has a DBID.
			var cloudCmd tea.Cmd
			if m.token != "" && n.DBID != "" {
				cloudCmd = m.cmdCloudPinToggle(n.DBID, !n.Pinned)
			}
			nm, reloadCmd := m.reloadNotes()
			return nm, tea.Batch(reloadCmd, cloudCmd)
		}

	case "/":
		m.mode = modeSearch

	case "esc":
		if m.searchQuery != "" {
			m.searchQuery = ""
			m.filteredNotes = m.allNotes
			m.cursor = 0
		}

	case "ctrl+r", "r":
		return m.reloadNotes()

	case "T":
		m.themeIndex = (m.themeIndex + 1) % len(AllThemes)
		m.theme = AllThemes[m.themeIndex]
		m.statusMsg = "Theme: " + m.theme.Name
		m.statusIsErr = false
		cmds := []tea.Cmd{
			clearStatusAfter(2 * time.Second),
			func() tea.Msg { return saveConfigMsg{theme: m.theme.Name} },
		}
		if m.token != "" {
			cmds = append(cmds, m.cmdCloudPatchTheme(m.theme.Name))
		}
		return m, tea.Batch(cmds...)

	case "V":
		m.mode = modeChangelog
		m.changelogOffset = 0
	}
	return m, nil
}

// ── Detail mode keys ─────────────────────────────────────────────────────────

func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "h", "left":
		m.mode = modeList

	case "ctrl+c":
		return m, tea.Quit

	case "j", "down":
		m.detailOffset++

	case "k", "up":
		if m.detailOffset > 0 {
			m.detailOffset--
		}

	case "g":
		m.detailOffset = 0

	case "G":
		m.detailOffset = 9999

	case "e":
		if len(m.filteredNotes) > 0 {
			n := m.filteredNotes[m.cursor]
			m.mode = modeEdit
			m.editID = n.ID
			m.editTitle = n.Title
			m.editBody = n.Body
			m.editTags = strings.Join(n.Tags, ", ")
			m.editField = 1
			m.editCursorPos = utf8.RuneCountInString(n.Body)
			m.editBodyOffset = 0
		}

	case "d":
		if len(m.filteredNotes) > 0 {
			m.mode = modeDelete
		}

	case "p":
		if len(m.filteredNotes) > 0 {
			n := m.filteredNotes[m.cursor]
			if err := m.store.TogglePin(n.ID); err != nil {
				return m, sendErr(err)
			}
			// Fire-and-forget cloud pin sync if logged in and note has a DBID.
			var cloudCmd tea.Cmd
			if m.token != "" && n.DBID != "" {
				cloudCmd = m.cmdCloudPinToggle(n.DBID, !n.Pinned)
			}
			nm, reloadCmd := m.reloadNotes()
			return nm, tea.Batch(reloadCmd, cloudCmd)
		}

	case "?":
		m.mode = modeHelp

	case "T":
		m.themeIndex = (m.themeIndex + 1) % len(AllThemes)
		m.theme = AllThemes[m.themeIndex]
		m.statusMsg = "Theme: " + m.theme.Name
		m.statusIsErr = false
		cmds := []tea.Cmd{
			clearStatusAfter(2 * time.Second),
			func() tea.Msg { return saveConfigMsg{theme: m.theme.Name} },
		}
		if m.token != "" {
			cmds = append(cmds, m.cmdCloudPatchTheme(m.theme.Name))
		}
		return m, tea.Batch(cmds...)

	case "V":
		m.mode = modeChangelog
		m.changelogOffset = 0
	}
	return m, nil
}

// ── Editor mode keys ─────────────────────────────────────────────────────────

func (m Model) handleEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		return m, nil

	case "ctrl+c":
		return m, tea.Quit

	case "ctrl+s":
		return m.saveNote()

	case "ctrl+w":
		// Save and return to detail
		nm, cmd := m.saveNote()
		if nm2, ok := nm.(Model); ok {
			nm2.mode = modeDetail
			return nm2, cmd
		}
		return nm, cmd

	case "tab":
		// Cycle: title(0) → body(1) → tags(2) → title(0)
		switch m.editField {
		case 0:
			m.editField = 1
			m.editCursorPos = utf8.RuneCountInString(m.editBody)
		case 1:
			m.editField = 2
			m.editCursorPos = utf8.RuneCountInString(m.editTags)
		case 2:
			m.editField = 0
			m.editCursorPos = utf8.RuneCountInString(m.editTitle)
		}

	default:
		switch m.editField {
		case 0:
			m.editTitle = handleTextInput(m.editTitle, &m.editCursorPos, msg)
		case 1:
			m.editBody = handleTextInput(m.editBody, &m.editCursorPos, msg)
		case 2:
			// Tags field is single-line: block newlines
			if msg.String() != "enter" {
				m.editTags = handleTextInput(m.editTags, &m.editCursorPos, msg)
			}
		}
	}
	return m, nil
}

// ── Search mode keys ─────────────────────────────────────────────────────────

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter":
		m.mode = modeList
		return m, nil

	case "ctrl+c":
		return m, tea.Quit

	case "backspace":
		if len(m.searchQuery) > 0 {
			runes := []rune(m.searchQuery)
			m.searchQuery = string(runes[:len(runes)-1])
		}

	default:
		if len(msg.Runes) > 0 {
			m.searchQuery += string(msg.Runes)
		}
	}
	m.filteredNotes = notes.FilterNotes(m.allNotes, m.searchQuery)
	m.cursor = 0
	m.detailOffset = 0
	return m, nil
}

// ── Delete mode keys ─────────────────────────────────────────────────────────

func (m Model) handleDeleteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if len(m.filteredNotes) > 0 {
			n := m.filteredNotes[m.cursor]
			if err := m.store.Delete(n.ID); err != nil {
				m.mode = modeList
				return m, sendErr(err)
			}
			m.mode = modeList
			m.statusMsg = "Note deleted"
			m.statusIsErr = false

			// Fire-and-forget cloud delete if logged in and note has a DBID.
			var cloudCmd tea.Cmd
			if m.token != "" && n.DBID != "" {
				cloudCmd = m.cmdCloudDelete(n.DBID)
			}
			nm, reloadCmd := m.reloadNotesWith(clearStatusAfter(2 * time.Second))
			return nm, tea.Batch(reloadCmd, cloudCmd)
		}
	case "n", "N", "esc", "ctrl+c":
		m.mode = modeList
	}
	return m, nil
}

// ── Help mode keys ────────────────────────────────────────────────────────────

func (m Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "?":
		m.mode = modeList
		m.helpOffset = 0
	case "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.helpOffset++
	case "k", "up":
		if m.helpOffset > 0 {
			m.helpOffset--
		}
	case "g", "home":
		m.helpOffset = 0
	case "G", "end":
		m.helpOffset = 9999
	}
	return m, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (m Model) saveNote() (tea.Model, tea.Cmd) {
	title := strings.TrimSpace(m.editTitle)
	if title == "" {
		title = "Untitled"
	}
	tags := parseTags(m.editTags)
	var (
		saved notes.Note
		err   error
	)
	if m.mode == modeNew || m.editID == "" {
		saved, err = m.store.Create(title, m.editBody, tags)
	} else {
		saved, err = m.store.Update(m.editID, title, m.editBody, tags)
	}
	if err != nil {
		return m, sendErr(err)
	}

	// Fire-and-forget cloud sync if the user is logged in.
	var cloudCmd tea.Cmd
	if m.email != "" {
		if m.mode == modeNew || m.editID == "" {
			cloudCmd = m.cmdCloudCreate(saved)
		} else {
			cloudCmd = m.cmdCloudPatch(saved)
		}
	}

	m.mode = modeList
	nm, reloadCmd := m.reloadNotesWith(func() tea.Msg { return savedMsg{} })
	return nm, tea.Batch(reloadCmd, cloudCmd)
}

// parseTags splits a comma-separated tag string into a cleaned slice.
// Empty entries and duplicates are removed.
func parseTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	seen := map[string]bool{}
	var tags []string
	for _, t := range strings.Split(raw, ",") {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			tags = append(tags, t)
		}
	}
	return tags
}

func (m Model) reloadNotes() (tea.Model, tea.Cmd) {
	return m.reloadNotesWith(nil)
}

func (m Model) reloadNotesWith(extra tea.Cmd) (tea.Model, tea.Cmd) {
	ns, err := m.store.Load()
	if err != nil {
		return m, sendErr(err)
	}
	m.allNotes = ns
	if m.searchQuery != "" {
		m.filteredNotes = notes.FilterNotes(ns, m.searchQuery)
	} else {
		m.filteredNotes = ns
	}
	if m.cursor >= len(m.filteredNotes) && len(m.filteredNotes) > 0 {
		m.cursor = len(m.filteredNotes) - 1
	}
	return m, extra
}

func sendErr(err error) tea.Cmd {
	return func() tea.Msg { return errMsg{err} }
}

// cmdCloudCreate returns a tea.Cmd that POSTs n to the cloud and writes back
// the DB id into the local store. On failure a cloudErrMsg is returned so the
// TUI can show a classified error in the status bar.
func (m Model) cmdCloudCreate(n notes.Note) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		c := cloud.New()
		tags := n.Tags
		if tags == nil {
			tags = []string{}
		}
		cn, err := c.CreateNote(ctx, m.token, cloud.CreateNoteRequest{
			Title:     n.Title,
			Body:      n.Body,
			Tags:      tags,
			Pinned:    n.Pinned,
			CreatedAt: &n.CreatedAt,
			UpdatedAt: &n.UpdatedAt,
		})
		if err != nil {
			return cloudErrMsg{text: cloud.ClassifyError(err)}
		}
		// Write the DBID back to disk so future edits can PATCH.
		_ = m.store.SetDBID(n.ID, cn.ID)
		return nil
	}
}

// cmdCloudPatch returns a tea.Cmd that PATCHes n in the cloud.
// If the note has no DBID yet it silently does nothing — a future sync
// will upload it. On failure a cloudErrMsg is returned.
func (m Model) cmdCloudPatch(n notes.Note) tea.Cmd {
	return func() tea.Msg {
		if n.DBID == "" {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		c := cloud.New()
		tags := n.Tags
		if tags == nil {
			tags = []string{}
		}
		title := n.Title
		body := n.Body
		pinned := n.Pinned
		_, err := c.PatchNote(ctx, m.token, n.DBID, cloud.PatchNoteRequest{
			Title:     &title,
			Body:      &body,
			Tags:      tags,
			Pinned:    &pinned,
			UpdatedAt: &n.UpdatedAt,
		})
		if err != nil {
			return cloudErrMsg{text: cloud.ClassifyError(err)}
		}
		return nil
	}
}

// cmdCloudDelete returns a tea.Cmd that sends DELETE /notes/{dbID} to the cloud.
// On failure a cloudErrMsg is returned so the TUI can show it in the status bar.
func (m Model) cmdCloudDelete(dbID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		c := cloud.New()
		if err := c.DeleteNote(ctx, m.token, dbID); err != nil {
			return cloudErrMsg{text: cloud.ClassifyError(err)}
		}
		return nil
	}
}

// cmdCloudPinToggle returns a tea.Cmd that PATCHes the pinned field for dbID.
// newPinned is the value AFTER the local toggle (i.e. what we want the server to store).
// On failure a cloudErrMsg is returned so the TUI can show it in the status bar.
func (m Model) cmdCloudPinToggle(dbID string, newPinned bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		c := cloud.New()
		_, err := c.PatchNote(ctx, m.token, dbID, cloud.PatchNoteRequest{
			Pinned: &newPinned,
		})
		if err != nil {
			return cloudErrMsg{text: cloud.ClassifyError(err)}
		}
		return nil
	}
}

// cmdCloudPatchTheme returns a tea.Cmd that PATCHes the user's theme preference
// on the cloud. Errors are surfaced as a cloudErrMsg in the status bar.
func (m Model) cmdCloudPatchTheme(name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		c := cloud.New()
		if _, err := c.PatchConfig(ctx, m.token, &name); err != nil {
			return cloudErrMsg{text: cloud.ClassifyError(err)}
		}
		return nil
	}
}

// cmdCloudSync returns a tea.Cmd that runs SyncWithCloud in a background
// goroutine and delivers the result as a syncDoneMsg.
func (m Model) cmdCloudSync() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := m.store.SyncWithCloud(ctx, m.token)
		return syncDoneMsg{result: result, err: err}
	}
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return statusClearMsg{} })
}

// handleTextInput processes keyboard input for a text field.
func handleTextInput(text string, cursorPos *int, msg tea.KeyMsg) string {
	runes := []rune(text)
	pos := *cursorPos
	if pos > len(runes) {
		pos = len(runes)
	}

	switch msg.String() {
	case "backspace":
		if pos > 0 {
			runes = append(runes[:pos-1], runes[pos:]...)
			pos--
		}
	case "delete":
		if pos < len(runes) {
			runes = append(runes[:pos], runes[pos+1:]...)
		}
	case "left", "ctrl+b":
		if pos > 0 {
			pos--
		}
	case "right", "ctrl+f":
		if pos < len(runes) {
			pos++
		}
	case "up":
		// Find start of current line
		lineStart := pos
		for lineStart > 0 && runes[lineStart-1] != '\n' {
			lineStart--
		}
		col := pos - lineStart
		if lineStart == 0 {
			// Already on the first line; move to start
			pos = 0
		} else {
			// Find start of previous line
			prevLineEnd := lineStart - 1 // points at the '\n'
			prevLineStart := prevLineEnd
			for prevLineStart > 0 && runes[prevLineStart-1] != '\n' {
				prevLineStart--
			}
			prevLineLen := prevLineEnd - prevLineStart
			if col > prevLineLen {
				col = prevLineLen
			}
			pos = prevLineStart + col
		}
	case "down":
		// Find end of current line
		lineStart := pos
		for lineStart > 0 && runes[lineStart-1] != '\n' {
			lineStart--
		}
		col := pos - lineStart
		lineEnd := pos
		for lineEnd < len(runes) && runes[lineEnd] != '\n' {
			lineEnd++
		}
		if lineEnd == len(runes) {
			// Already on the last line; move to end
			pos = len(runes)
		} else {
			// Find end of next line
			nextLineStart := lineEnd + 1
			nextLineEnd := nextLineStart
			for nextLineEnd < len(runes) && runes[nextLineEnd] != '\n' {
				nextLineEnd++
			}
			nextLineLen := nextLineEnd - nextLineStart
			if col > nextLineLen {
				col = nextLineLen
			}
			pos = nextLineStart + col
		}
	case "home", "ctrl+a":
		// go to start of current line
		for pos > 0 && runes[pos-1] != '\n' {
			pos--
		}
	case "end", "ctrl+e":
		// go to end of current line
		for pos < len(runes) && runes[pos] != '\n' {
			pos++
		}
	case "enter":
		runes = append(runes[:pos], append([]rune{'\n'}, runes[pos:]...)...)
		pos++
	case "ctrl+k":
		// kill to end of line
		end := pos
		for end < len(runes) && runes[end] != '\n' {
			end++
		}
		runes = append(runes[:pos], runes[end:]...)
	default:
		if len(msg.Runes) > 0 {
			runes = append(runes[:pos], append(msg.Runes, runes[pos:]...)...)
			pos += len(msg.Runes)
		}
	}

	*cursorPos = pos
	return string(runes)
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	// Heights
	headerH := 1
	statusH := 1
	innerH := m.height - headerH - statusH - 2 // 2 for borders

	// Widths — list takes 30%, detail takes the rest
	listW := max(22, m.width*30/100)
	detailW := m.width - listW - 3 // gap + borders

	header := m.renderHeader()
	listPanel := m.renderList(listW, innerH)
	detailPanel := m.renderDetail(detailW, innerH)
	status := m.renderStatus()

	body := lipgloss.JoinHorizontal(lipgloss.Top, listPanel, " ", detailPanel)

	bg := lipgloss.JoinVertical(lipgloss.Left,
		header,
		body,
		status,
	)

	// Help overlay: render dialog and place it centered over the background.
	if m.mode == modeHelp {
		dialog := m.renderHelp()
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			dialog,
		)
	}

	// Changelog overlay: same floating pattern as help.
	if m.mode == modeChangelog {
		dialog := m.renderChangelog()
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			dialog,
		)
	}

	return bg
}

func (m Model) renderHeader() string {
	th := m.theme

	// Logo: "◆ nnn.rocks" when logged in, "◆ nnn" otherwise.
	logoText := "◆ nnn"
	if m.email != "" {
		logoText = "◆ nnn.rocks"
	}
	logo := th.AppHeader.Render(logoText)
	desc := th.AppVersion.Render("an elegant TUI note manager")
	sep := th.StatusSep.Render(" │ ")
	modeStr := ""
	switch m.mode {
	case modeEdit:
		modeStr = th.StatusKey.Render(" EDIT")
	case modeNew:
		modeStr = th.StatusKey.Render(" NEW")
	case modeSearch:
		modeStr = th.SearchActive.Render(" SEARCH: " + m.searchQuery + "█")
	case modeDelete:
		modeStr = th.StatusErr.Render(" DELETE?")
	case modeChangelog:
		modeStr = th.StatusKey.Render(" CHANGELOG")
	}

	left := logo + sep + desc + modeStr

	// Right side: version, theme, and — when logged in — the user's email.
	ver := strings.TrimPrefix(m.version, "v")
	if ver == "" {
		ver = "dev"
	}
	rightStr := th.AppVersion.Render("v"+ver) +
		th.StatusSep.Render(" │ ") +
		th.AppHeader.Render(m.theme.Name)
	if m.email != "" {
		rightStr += th.StatusSep.Render(" │ ") +
			th.AppVersion.Render(m.email)
	}

	// Pad left side to push right side to the terminal edge
	leftLen := lipgloss.Width(left)
	rightLen := lipgloss.Width(rightStr)
	gap := m.width - leftLen - rightLen
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + rightStr
}

func (m Model) renderList(w, h int) string {
	th := m.theme
	focused := m.mode == modeList || m.mode == modeSearch || m.mode == modeDelete

	// Inner width for content (border takes 2 chars each side = 2 total per side)
	innerW := w - 4 // 2 border + 2 padding

	var rows []string

	// Count header
	countStr := fmt.Sprintf("%d notes", len(m.filteredNotes))
	if m.searchQuery != "" {
		countStr = fmt.Sprintf("%d/%d", len(m.filteredNotes), len(m.allNotes))
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		th.ListHeader.Render("Notes"),
		" ",
		th.ListCount.Render(countStr),
	)
	rows = append(rows, header)
	rows = append(rows, th.ListCount.Render(strings.Repeat("─", innerW)))

	if len(m.filteredNotes) == 0 {
		rows = append(rows, th.Empty.Width(innerW).Render("no notes"))
	}

	for i, n := range m.filteredNotes {
		pin := th.ListItemNormal.String()
		if n.Pinned {
			pin = th.ListItemPinned.String()
		}
		title := n.Title
		if title == "" {
			title = "(untitled)"
		}
		date := n.UpdatedAt.Format("Jan 02")
		dateRendered := th.ListCount.Render(date)
		dateW := lipgloss.Width(dateRendered)

		// innerW already excludes the 1-char left padding from the item style,
		// but the item style's Padding(0,1) adds 1 on each side = 2 total.
		// We work in the content width (innerW - 2 for item padding) so that
		// when the full line is passed to the item style the date lands flush right.
		contentW := innerW - 2 // account for Padding(0,1) on item styles
		pinW := lipgloss.Width(pin)
		maxTitleW := contentW - pinW - dateW
		if maxTitleW < 1 {
			maxTitleW = 1
		}
		titleRunes := []rune(title)
		if len(titleRunes) > maxTitleW {
			titleRunes = append(titleRunes[:maxTitleW-1], '…')
		}
		title = string(titleRunes)

		// Pad the title so date is always right-aligned.
		titleW := utf8.RuneCountInString(title)
		gap := contentW - pinW - titleW - dateW
		if gap < 0 {
			gap = 0
		}
		fullLine := pin + title + strings.Repeat(" ", gap) + dateRendered

		if i == m.cursor {
			rows = append(rows, th.ListItemSelected.Width(innerW).Render(fullLine))
		} else {
			rows = append(rows, th.ListItem.Width(innerW).Render(fullLine))
		}
	}

	content := strings.Join(rows, "\n")

	// Clip to height
	lines := strings.Split(content, "\n")
	visibleH := h
	if len(lines) > visibleH {
		// scroll so cursor is visible
		start := 0
		cursorLine := m.cursor + 2 // +2 for header rows
		if cursorLine >= visibleH {
			start = cursorLine - visibleH + 1
		}
		end := start + visibleH
		if end > len(lines) {
			end = len(lines)
		}
		lines = lines[start:end]
	}
	// Pad to height
	for len(lines) < visibleH {
		lines = append(lines, "")
	}
	content = strings.Join(lines, "\n")

	if focused {
		return th.ListPanelFoc.Width(w).Height(h).Render(content)
	}
	return th.ListPanel.Width(w).Height(h).Render(content)
}

func (m Model) renderDetail(w, h int) string {
	th := m.theme
	focused := m.mode == modeDetail

	innerW := w - 4

	if m.mode == modeEdit || m.mode == modeNew {
		return m.renderEditor(w, h)
	}

	if len(m.filteredNotes) == 0 {
		content := th.Empty.Width(innerW).Height(h).
			Render("No notes yet.\nPress  n  to create one.")
		return th.DetailPanel.Width(w).Height(h).Render(content)
	}

	n := m.filteredNotes[m.cursor]

	var parts []string

	// Title
	titleText := n.Title
	if titleText == "" {
		titleText = "(untitled)"
	}
	parts = append(parts, th.DetailTitle.Width(innerW).Render(titleText))

	// Tags
	if len(n.Tags) > 0 {
		var tagChips []string
		for _, t := range n.Tags {
			tagChips = append(tagChips, th.Tag.Render("#"+t))
		}
		parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Top, tagChips...))
	}

	// Meta
	created := n.CreatedAt.Format("Jan 02, 2006 15:04")
	updated := n.UpdatedAt.Format("Jan 02, 2006 15:04")
	metaLine := th.DetailMeta.Render(fmt.Sprintf("created %s  ·  updated %s", created, updated))
	if n.Pinned {
		metaLine = lipgloss.JoinHorizontal(lipgloss.Top,
			th.ListItemPinned.String()+" ",
			metaLine,
		)
	}
	parts = append(parts, metaLine)
	parts = append(parts, th.DetailMeta.Render(strings.Repeat("─", innerW)))

	// Body
	body := n.Body
	if body == "" {
		parts = append(parts, th.DetailBody.Width(innerW).Render("(empty)"))
	} else {
		rendered := body
		if renderer, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle(m.theme.GlamourStyle),
			glamour.WithWordWrap(innerW),
		); err == nil {
			if out, err := renderer.Render(body); err == nil {
				rendered = strings.TrimRight(out, "\n")
			}
		}
		parts = append(parts, rendered)
	}

	content := strings.Join(parts, "\n")

	// Apply scroll offset
	lines := strings.Split(content, "\n")
	off := m.detailOffset
	if off > len(lines)-1 {
		off = len(lines) - 1
	}
	if off < 0 {
		off = 0
	}
	end := off + h
	if end > len(lines) {
		end = len(lines)
	}
	visible := strings.Join(lines[off:end], "\n")

	if focused {
		return th.DetailPanelFoc.Width(w).Height(h).Render(visible)
	}
	return th.DetailPanel.Width(w).Height(h).Render(visible)
}

func (m Model) renderEditor(w, h int) string {
	th := m.theme
	innerW := w - 4

	// ── helper: render a field with cursor if active ───────────────────────
	renderField := func(text string, fieldIdx int, placeholder string) string {
		runes := []rune(text)
		if m.editField != fieldIdx {
			s := string(runes)
			if s == "" {
				return th.DetailMeta.Render(placeholder)
			}
			return s
		}
		pos := m.editCursorPos
		if pos > len(runes) {
			pos = len(runes)
		}
		before := string(runes[:pos])
		if pos < len(runes) {
			ch := runes[pos]
			if ch == '\n' {
				// Cursor is on an empty line: show a space block then the newline
				return before + th.Cursor.Render(" ") + "\n" + string(runes[pos+1:])
			}
			cur := th.Cursor.Render(string(ch))
			after := string(runes[pos+1:])
			return before + cur + after
		}
		return before + th.Cursor.Render(" ")
	}

	// ── fields ────────────────────────────────────────────────────────────
	titleActive := m.editField == 0
	bodyActive := m.editField == 1
	tagsActive := m.editField == 2

	labelTitle := th.EditorTitleLabel.Render("Title")
	labelBody := th.EditorBodyLabel.Render("Body ")
	labelTags := th.EditorBodyLabel.Render("Tags ")
	if titleActive {
		labelTitle = th.EditorTitleLabel.Render("Title")
	}
	if bodyActive {
		labelBody = th.EditorTitleLabel.Render("Body ")
	}
	if tagsActive {
		labelTags = th.EditorTitleLabel.Render("Tags ")
	}

	titleStr := renderField(m.editTitle, 0, "(title)")
	bodyStr := renderField(m.editBody, 1, "(body)")
	tagsStr := renderField(m.editTags, 2, "(comma-separated, e.g. work, ideas)")

	sep := th.DetailMeta.Render(strings.Repeat("─", innerW))
	hint := th.DetailMeta.Render("tab: next field  ·  ctrl+s: save  ·  ctrl+w: save & view  ·  esc: cancel")

	titleLine := lipgloss.JoinHorizontal(lipgloss.Top, labelTitle, ": ", titleStr)
	tagsLine := lipgloss.JoinHorizontal(lipgloss.Top, labelTags, ": ", tagsStr)

	content := strings.Join([]string{
		titleLine,
		sep,
		lipgloss.JoinHorizontal(lipgloss.Top, labelBody, ": "),
		bodyStr,
		sep,
		tagsLine,
		"",
		hint,
	}, "\n")

	return th.EditorPanel.Width(w).Height(h).Render(content)
}

func (m Model) renderStatus() string {
	th := m.theme
	if m.statusMsg != "" {
		if m.statusIsErr {
			return th.StatusErr.Render("✗ " + m.statusMsg)
		}
		return th.StatusMsg.Render("✓ " + m.statusMsg)
	}

	sep := th.StatusSep.Render(" · ")

	switch m.mode {
	case modeList:
		keys := []string{
			th.StatusKey.Render("j/k") + " nav",
			th.StatusKey.Render("n") + " new",
			th.StatusKey.Render("e") + " edit",
			th.StatusKey.Render("d") + " del",
			th.StatusKey.Render("p") + " pin",
			th.StatusKey.Render("/") + " search",
			th.StatusKey.Render("T") + " theme",
			th.StatusKey.Render("V") + " changelog",
			th.StatusKey.Render("?") + " help",
			th.StatusKey.Render("q") + " quit",
		}
		return th.StatusBar.Render(strings.Join(keys, sep))
	case modeDetail:
		keys := []string{
			th.StatusKey.Render("j/k") + " scroll",
			th.StatusKey.Render("e") + " edit",
			th.StatusKey.Render("d") + " del",
			th.StatusKey.Render("T") + " theme",
			th.StatusKey.Render("V") + " changelog",
			th.StatusKey.Render("esc/h") + " back",
		}
		return th.StatusBar.Render(strings.Join(keys, sep))
	case modeEdit, modeNew:
		fieldName := []string{"title", "body", "tags"}[m.editField]
		keys := []string{
			th.StatusKey.Render("tab") + " next field",
			th.StatusKey.Render("ctrl+s") + " save",
			th.StatusKey.Render("esc") + " cancel",
			th.DetailMeta.Render("editing: ") + th.StatusKey.Render(fieldName),
		}
		return th.StatusBar.Render(strings.Join(keys, sep))
	case modeSearch:
		return th.SearchActive.Render("  Searching: " + m.searchQuery + "█  ·  enter/esc to confirm")
	case modeDelete:
		n := m.filteredNotes[m.cursor]
		title := n.Title
		if title == "" {
			title = "(untitled)"
		}
		return th.StatusErr.Render(fmt.Sprintf("  Delete \"%s\"? y/n", title))
	}
	return ""
}

func (m Model) renderHelp() string {
	th := m.theme

	// ── Dialog dimensions (capped so it floats, not full-screen) ─────────────
	dialogW := m.width - 8
	if dialogW > 110 {
		dialogW = 110
	}
	if dialogW < 30 {
		dialogW = 30
	}
	dialogH := m.height - 6
	if dialogH > 42 {
		dialogH = 42
	}
	if dialogH < 10 {
		dialogH = 10
	}

	// ── Total inner width available (overlay has 2-char border + 3 padding each side = 8) ─
	innerW := dialogW - 8
	if innerW < 20 {
		innerW = 20
	}
	sepW := 1
	halfW := (innerW - sepW) / 2
	leftW := halfW
	rightW := innerW - sepW - leftW

	// ── Left column: keyboard shortcuts ──────────────────────────────────────
	type binding struct{ key, desc string }
	sections := []struct {
		header   string
		bindings []binding
	}{
		{"Navigation", []binding{
			{"j / ↓", "Move down"},
			{"k / ↑", "Move up"},
			{"g / Home", "Go to top"},
			{"G / End", "Go to bottom"},
			{"Enter / l", "Open note"},
			{"h / Esc", "Back to list"},
		}},
		{"Notes", []binding{
			{"n", "New note"},
			{"e", "Edit note"},
			{"d", "Delete note (confirm)"},
			{"p", "Toggle pin"},
			{"r", "Reload from disk"},
		}},
		{"Search", []binding{
			{"/", "Start search"},
			{"Esc", "Clear search"},
		}},
		{"Editor", []binding{
			{"Tab", "Cycle title → body → tags"},
			{"Ctrl+S", "Save note"},
			{"Ctrl+W", "Save & view"},
			{"Ctrl+K", "Kill to end of line"},
			{"Ctrl+A / Home", "Start of line"},
			{"Ctrl+E / End", "End of line"},
			{"Esc", "Cancel edit"},
		}},
		{"App", []binding{
			{"T", "Cycle theme (" + m.theme.Name + ")"},
			{"V", "What's new (changelog)"},
			{"?", "Toggle help"},
			{"q / Ctrl+C", "Quit"},
		}},
	}

	const keyColW = 16 // fixed width for the key column in shortcut rows

	var leftRows []string
	leftRows = append(leftRows, th.HelpTitle.Render("Keyboard shortcuts"), "")
	for _, sec := range sections {
		leftRows = append(leftRows, th.HelpTitle.Render(sec.header))
		for _, b := range sec.bindings {
			keyRendered := th.HelpKey.Render(b.key)
			// Pad the key cell to keyColW so descriptions align regardless of key length.
			pad := keyColW - lipgloss.Width(keyRendered)
			if pad < 1 {
				pad = 1
			}
			leftRows = append(leftRows, keyRendered+strings.Repeat(" ", pad)+th.HelpDesc.Render(b.desc))
		}
		leftRows = append(leftRows, "")
	}

	// ── Right column: about ───────────────────────────────────────────────────
	sep := th.DetailMeta.Render("│")

	// wordWrap wraps text to fit within w runes, returning one string per line.
	wordWrap := func(text string, w int) []string {
		if w < 1 {
			return []string{text}
		}
		var lines []string
		for _, paragraph := range strings.Split(text, "\n") {
			words := strings.Fields(paragraph)
			if len(words) == 0 {
				lines = append(lines, "")
				continue
			}
			line := ""
			for _, word := range words {
				if line == "" {
					line = word
				} else if utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= w {
					line += " " + word
				} else {
					lines = append(lines, line)
					line = word
				}
			}
			if line != "" {
				lines = append(lines, line)
			}
		}
		return lines
	}

	wrapW := rightW - 2 // leave a little breathing room
	if wrapW < 10 {
		wrapW = 10
	}

	var rightRows []string
	rightRows = append(rightRows, th.HelpTitle.Render("About nnn"), "")

	about := "nnn is a keyboard-driven TUI for managing notes in the terminal — built with Bubble Tea and lipgloss, because plain text and fast navigation are all you really need."
	for _, l := range wordWrap(about, wrapW) {
		rightRows = append(rightRows, th.HelpDesc.Render(l))
	}
	rightRows = append(rightRows, "", "")

	cloud := "Notes can be synced to nnn.rocks, a hosted backend that keeps your notes in sync across devices and machines."
	for _, l := range wordWrap(cloud, wrapW) {
		rightRows = append(rightRows, th.HelpDesc.Render(l))
	}
	rightRows = append(rightRows, "", "")

	if m.email != "" {
		rightRows = append(rightRows, th.HelpTitle.Render("Cloud sync"), "")
		rightRows = append(rightRows, th.HelpDesc.Render("Signed in as:"))
		rightRows = append(rightRows, th.HelpKey.Render(m.email))
		rightRows = append(rightRows, "")
		for _, l := range wordWrap("Notes sync automatically on startup. Every edit, create, delete, and pin change is pushed in the background.", wrapW) {
			rightRows = append(rightRows, th.HelpDesc.Render(l))
		}
	} else {
		rightRows = append(rightRows, th.HelpTitle.Render("Cloud sync"), "")
		for _, l := range wordWrap("Sign up at nnn.rocks, then connect your account:", wrapW) {
			rightRows = append(rightRows, th.HelpDesc.Render(l))
		}
		rightRows = append(rightRows, "")
		rightRows = append(rightRows, th.HelpKey.Render("nnn auth login"))
		rightRows = append(rightRows, "")
		for _, l := range wordWrap("Your notes will sync across all your devices automatically.", wrapW) {
			rightRows = append(rightRows, th.HelpDesc.Render(l))
		}
	}
	rightRows = append(rightRows, "", "")
	rightRows = append(rightRows, th.DetailMeta.Render("made with ♥ by Antonio Davide Calì"))

	// ── Zip columns into rows ─────────────────────────────────────────────────
	totalRows := len(leftRows)
	if len(rightRows) > totalRows {
		totalRows = len(rightRows)
	}
	// Pad both columns to the same height.
	for len(leftRows) < totalRows {
		leftRows = append(leftRows, "")
	}
	for len(rightRows) < totalRows {
		rightRows = append(rightRows, "")
	}

	rows := make([]string, totalRows)
	for i := range rows {
		// Truncate each cell to its column width without filling background.
		lText := leftRows[i]
		if lipgloss.Width(lText) > leftW {
			lText = lipgloss.NewStyle().MaxWidth(leftW).Render(lText)
		}
		// Right-pad with plain spaces (no style) so the separator stays aligned.
		lPad := leftW - lipgloss.Width(lText)
		if lPad > 0 {
			lText += strings.Repeat(" ", lPad)
		}

		rText := rightRows[i]
		if lipgloss.Width(rText) > rightW {
			rText = lipgloss.NewStyle().MaxWidth(rightW).Render(rText)
		}

		rows[i] = lText + sep + rText
	}

	// Append scroll hint as a full-width footer row.
	rows = append(rows, th.DetailMeta.Render("j/k: scroll  ·  g/G: top/bottom  ·  esc/?/q: close"))
	totalRows = len(rows)

	// ── Scroll / clip ─────────────────────────────────────────────────────────
	// helpOverhead accounts for the border (2) + padding (0) of HelpOverlay.
	const helpOverhead = 2
	visible := dialogH - helpOverhead
	if visible < 1 {
		visible = 1
	}

	off := m.helpOffset
	maxOff := totalRows - visible
	if maxOff < 0 {
		maxOff = 0
	}
	if off > maxOff {
		off = maxOff
	}
	if off < 0 {
		off = 0
	}

	end := off + visible
	if end > totalRows {
		end = totalRows
	}
	slice := rows[off:end]

	for len(slice) < visible {
		slice = append(slice, "")
	}

	// Scroll indicator in the top-right corner.
	if totalRows > visible {
		indicator := th.DetailMeta.Render(fmt.Sprintf("%d%%", (off+1)*100/totalRows))
		titleRow := slice[0]
		pad := innerW - lipgloss.Width(titleRow) - lipgloss.Width(indicator)
		if pad > 0 {
			slice[0] = titleRow + strings.Repeat(" ", pad) + indicator
		}
	}

	content := strings.Join(slice, "\n")
	return th.HelpOverlay.Width(dialogW - 2).Height(dialogH - 2).Render(content)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ── Changelog overlay ─────────────────────────────────────────────────────────

func (m Model) handleChangelogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "V", "enter":
		m.mode = modeList
		m.changelogOffset = 0
		ver := strings.TrimPrefix(m.version, "v")
		return m, func() tea.Msg { return saveChangelogSeenMsg{version: ver} }
	case "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.changelogOffset++
	case "k", "up":
		if m.changelogOffset > 0 {
			m.changelogOffset--
		}
	case "g", "home":
		m.changelogOffset = 0
	case "G", "end":
		m.changelogOffset = 9999
	}
	return m, nil
}

func (m Model) renderChangelog() string {
	th := m.theme

	dialogW := m.width - 8
	if dialogW > 90 {
		dialogW = 90
	}
	if dialogW < 30 {
		dialogW = 30
	}
	dialogH := m.height - 8
	if dialogH > 30 {
		dialogH = 30
	}
	if dialogH < 8 {
		dialogH = 8
	}

	innerW := dialogW - 8
	if innerW < 20 {
		innerW = 20
	}

	ver := strings.TrimPrefix(m.version, "v")
	entries := changelogEntries

	var rows []string
	rows = append(rows, th.HelpTitle.Render("What's new in v"+ver), "")

	for _, entry := range entries {
		wrapW := innerW - 3
		if wrapW < 10 {
			wrapW = 10
		}
		words := strings.Fields(entry)
		line := ""
		first := true
		for _, word := range words {
			if line == "" {
				line = word
			} else if utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= wrapW {
				line += " " + word
			} else {
				prefix := "   "
				if first {
					prefix = th.StatusKey.Render("·") + "  "
					first = false
				}
				rows = append(rows, prefix+th.HelpDesc.Render(line))
				line = word
			}
		}
		if line != "" {
			prefix := "   "
			if first {
				prefix = th.StatusKey.Render("·") + "  "
			}
			rows = append(rows, prefix+th.HelpDesc.Render(line))
		}
		rows = append(rows, "")
	}

	rows = append(rows, th.DetailMeta.Render("esc / q / enter: close  ·  j/k: scroll  ·  V: reopen anytime"))

	totalRows := len(rows)

	const overhead = 2
	visible := dialogH - overhead
	if visible < 1 {
		visible = 1
	}

	off := m.changelogOffset
	maxOff := totalRows - visible
	if maxOff < 0 {
		maxOff = 0
	}
	if off > maxOff {
		off = maxOff
	}
	if off < 0 {
		off = 0
	}
	end := off + visible
	if end > totalRows {
		end = totalRows
	}
	slice := rows[off:end]
	for len(slice) < visible {
		slice = append(slice, "")
	}

	if totalRows > visible {
		indicator := th.DetailMeta.Render(fmt.Sprintf("%d%%", (off+1)*100/totalRows))
		titleRow := slice[0]
		pad := innerW - lipgloss.Width(titleRow) - lipgloss.Width(indicator)
		if pad > 0 {
			slice[0] = titleRow + strings.Repeat(" ", pad) + indicator
		}
	}

	content := strings.Join(slice, "\n")
	return th.HelpOverlay.Width(dialogW - 2).Height(dialogH - 2).Render(content)
}
