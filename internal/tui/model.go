package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
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
	modeList             mode = iota // navigating the list
	modeDetail                       // reading detail (right panel focused)
	modeEdit                         // editing an existing note
	modeNew                          // creating a new note
	modeSearch                       // typing a search query
	modeDelete                       // confirm delete
	modeHelp                         // help overlay
	modeChangelogLatest              // what's new overlay
	modeChangelogHistory             // Full changelog
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

	// word selection (double-click to highlight) within the editor
	selField int // field the selection belongs to (0/1/2); -1 = no selection
	selStart int // rune offset of the selection's start within that field
	selEnd   int // rune offset of the selection's end (exclusive) within that field

	// double-click detection for the editor
	lastClickField int       // field of the previous mouse press; -1 if none yet
	lastClickPos   int       // rune offset of the previous mouse press
	lastClickAt    time.Time // wall-clock time of the previous mouse press

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
		store:          store,
		allNotes:       ns,
		filteredNotes:  ns,
		theme:          AllThemes[idx],
		themeIndex:     idx,
		version:        version,
		email:          email,
		token:          token,
		showChangelog:  showChangelog,
		selField:       -1,
		lastClickField: -1,
	}
	if showChangelog {
		m.mode = modeChangelogLatest
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

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if m.mode == modeEdit || m.mode == modeNew {
				return m.handleEditorClick(msg.X, msg.Y)
			}
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
	case modeChangelogLatest:
		return m.handleChangelogKey(msg)
	case modeChangelogHistory:
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
		m.selField = -1

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
			m.selField = -1
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
		m.mode = modeChangelogHistory
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
			m.selField = -1
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
		m.mode = modeChangelogHistory
		m.changelogOffset = 0
	}
	return m, nil
}

// ── Editor mode keys ─────────────────────────────────────────────────────────

func (m Model) handleEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Snapshot any active word highlight before clearing it: ctrl+b/ctrl+u
	// consume it below (wrapping the highlighted word) if that's what this
	// key turns out to be. Every other key just drops it — there's no
	// click-and-type replace-selection behavior beyond that.
	selField, selStart, selEnd := m.selField, m.selStart, m.selEnd
	m.selField = -1

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

	case "ctrl+b":
		// Bold — body field only. Wraps a double-click-highlighted word if
		// there is one, otherwise the empty-span cursor behavior.
		if m.editField == 1 {
			if selField == 1 && selStart < selEnd {
				m.editBody, m.editCursorPos = wrapSelection(m.editBody, selStart, selEnd, "**")
			} else {
				m.editBody = toggleWrap(m.editBody, &m.editCursorPos, "**")
			}
		}

	case "ctrl+u":
		// Italic — body field only. Wraps a double-click-highlighted word if
		// there is one, otherwise the empty-span cursor behavior.
		if m.editField == 1 {
			if selField == 1 && selStart < selEnd {
				m.editBody, m.editCursorPos = wrapSelection(m.editBody, selStart, selEnd, "_")
			} else {
				m.editBody = toggleWrap(m.editBody, &m.editCursorPos, "_")
			}
		}

	case "ctrl+t":
		// Cycle the current line through H1 → H2 → H3 → plain — body field only.
		if m.editField == 1 {
			m.editBody = cycleHeading(m.editBody, &m.editCursorPos)
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

// ── Editor mouse click ───────────────────────────────────────────────────────

// doubleClickWithin is the maximum gap between two presses at the same
// character for the second one to count as a double-click.
const doubleClickWithin = 400 * time.Millisecond

// labelPrefixW is the visual width of the "Title: " / "Tags : " prefix that
// precedes the first row of those fields.
const labelPrefixW = 7

// editorRow describes one on-screen row inside the editor panel.
// field is -1 for decorative rows (separators) that don't belong to any
// editable field and can't be clicked.
type editorRow struct {
	field    int    // 0 = title, 1 = body, 2 = tags; -1 = decorative
	text     string // this row's rune content, unstyled (valid when field >= 0)
	startPos int    // rune offset into the field's full text where this row begins
}

// editorFieldWidths derives, from the editor's full content width wrapW,
// the wrap width for the title/tags fields (which share a row with a
// 7-column label) and for the body field (which doesn't). Both reserve one
// extra column beyond what the text itself needs so a cursor sitting right
// after the last character of a maximally-wide row always has room to draw
// without pushing that row's rendered width past wrapW — which would make
// lipgloss's own word-wrap silently reflow it, throwing off every row
// below. renderEditor and editorRows both call this so their notions of
// where each row starts and ends can never drift apart.
func editorFieldWidths(wrapW int) (labeledW, bodyW int) {
	if wrapW < 1 {
		wrapW = 1
	}
	labeledW = wrapW - labelPrefixW - 1
	if labeledW < 1 {
		labeledW = 1
	}
	bodyW = wrapW - 1
	if bodyW < 1 {
		bodyW = 1
	}
	return labeledW, bodyW
}

// editorRows lays out the title/body/tags fields into on-screen rows,
// soft-wrapping each field to wrapW columns. It is the single source of
// truth for the editor's geometry: both renderEditor (to know where to draw
// the cursor and selection) and handleEditorClick (to invert a screen
// position back into a field + rune offset) build off of it, so the two can
// never drift out of sync the way raw-versus-wrapped row counting once did.
func (m Model) editorRows(wrapW int) []editorRow {
	fieldW, bodyW := editorFieldWidths(wrapW)

	var rows []editorRow

	titleLines, titleStarts := wrapWithOffsets(m.editTitle, fieldW)
	for i, l := range titleLines {
		rows = append(rows, editorRow{field: 0, text: l, startPos: titleStarts[i]})
	}

	rows = append(rows, editorRow{field: -1}) // separator
	// "Body : " label sits on its own row; clicking it jumps to the body's start.
	rows = append(rows, editorRow{field: 1, text: "", startPos: 0})

	bodyLines, bodyStarts := wrapWithOffsets(m.editBody, bodyW)
	for i, l := range bodyLines {
		rows = append(rows, editorRow{field: 1, text: l, startPos: bodyStarts[i]})
	}

	rows = append(rows, editorRow{field: -1}) // separator

	tagsLines, tagsStarts := wrapWithOffsets(m.editTags, fieldW)
	for i, l := range tagsLines {
		rows = append(rows, editorRow{field: 2, text: l, startPos: tagsStarts[i]})
	}

	return rows
}

// wrapWithOffsets soft-wraps text to fit within width runes per visual line,
// breaking at spaces like a normal text editor and hard-breaking a single
// word that's longer than width on its own. Alongside each visual line it
// returns the rune offset into text where that line begins, so a screen
// position can be mapped back to an exact cursor offset (and vice versa)
// regardless of how the text happened to wrap.
func wrapWithOffsets(text string, width int) (lines []string, starts []int) {
	if width < 1 {
		width = 1
	}
	runes := []rune(text)
	n := len(runes)

	pos := 0
	for {
		lineEnd := pos
		for lineEnd < n && runes[lineEnd] != '\n' {
			lineEnd++
		}

		segStart := pos
		for {
			remaining := lineEnd - segStart
			if remaining <= width {
				lines = append(lines, string(runes[segStart:lineEnd]))
				starts = append(starts, segStart)
				segStart = lineEnd
				break
			}

			breakAt := segStart + width
			scan := breakAt
			for scan > segStart && runes[scan] != ' ' {
				scan--
			}
			if scan == segStart {
				scan = breakAt // no space to break on — hard break at width
			}

			lines = append(lines, string(runes[segStart:scan]))
			starts = append(starts, segStart)

			next := scan
			if next < lineEnd && runes[next] == ' ' {
				next++ // consume the space that caused the break
			}
			segStart = next
		}

		if lineEnd == n {
			break
		}
		pos = lineEnd + 1 // skip the '\n'
	}

	return lines, starts
}

// fieldText returns the current text of the given editor field (0/1/2).
func (m Model) fieldText(field int) string {
	switch field {
	case 0:
		return m.editTitle
	case 2:
		return m.editTags
	default:
		return m.editBody
	}
}

// isWordRune reports whether r counts as part of a "word" for double-click
// selection purposes.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// wordBoundsAt returns the rune-offset span [start, end) of the word
// touching cursor position pos in text — checking the character right at
// pos first, then the one just before it, so clicking anywhere inside or at
// either edge of a word selects it. If neither side is a word character,
// start == end and there is nothing to select.
func wordBoundsAt(text string, pos int) (start, end int) {
	runes := []rune(text)
	n := len(runes)

	i := pos
	if i >= n || !isWordRune(runes[i]) {
		i = pos - 1
	}
	if i < 0 || i >= n || !isWordRune(runes[i]) {
		return pos, pos
	}

	start = i
	for start > 0 && isWordRune(runes[start-1]) {
		start--
	}
	end = i + 1
	for end < n && isWordRune(runes[end]) {
		end++
	}
	return start, end
}

// handleEditorClick repositions the editor cursor to the character that was
// clicked, translating screen coordinates into a (field, cursorPos) pair via
// editorRows. A second click on the same character within doubleClickWithin
// selects (highlights) the whole word under it.
func (m Model) handleEditorClick(mx, my int) (tea.Model, tea.Cmd) {
	listW := max(22, m.width*30/100)
	detailW := m.width - listW - 3

	// Editor panel content origin on screen:
	//   X = listW (list panel) + 1 (gap) + 1 (border) + 1 (padding)
	//   Y = 1 (header) + 1 (top border)
	contentOriginX := listW + 3
	contentOriginY := 2

	col := mx - contentOriginX
	row := my - contentOriginY
	if col < 0 || row < 0 {
		return m, nil
	}

	wrapW := detailW - 2 // content columns available inside the border + padding
	rows := m.editorRows(wrapW)
	if row >= len(rows) {
		return m, nil
	}
	r := rows[row]
	if r.field == -1 {
		return m, nil
	}

	// Every title/tags row (including wrapped continuation rows, which
	// renderEditor pads to match) is drawn labelPrefixW columns in from the
	// body's — body rows have no label of their own.
	colInLine := col
	if r.field != 1 {
		colInLine -= labelPrefixW
	}
	if colInLine < 0 {
		colInLine = 0
	}
	lineRuneCount := utf8.RuneCountInString(r.text)
	if colInLine > lineRuneCount {
		colInLine = lineRuneCount
	}
	pos := r.startPos + colInLine

	now := time.Now()
	isDoubleClick := m.lastClickField == r.field &&
		m.lastClickPos == pos &&
		!m.lastClickAt.IsZero() &&
		now.Sub(m.lastClickAt) <= doubleClickWithin

	m.editField = r.field
	m.editCursorPos = pos

	if isDoubleClick {
		start, end := wordBoundsAt(m.fieldText(r.field), pos)
		if start < end {
			m.selField = r.field
			m.selStart = start
			m.selEnd = end
			m.editCursorPos = end
		}
		// Consume the pair so a third press starts a fresh double-click
		// window instead of immediately re-triggering.
		m.lastClickField = -1
		m.lastClickAt = time.Time{}
	} else {
		m.selField = -1
		m.lastClickField = r.field
		m.lastClickPos = pos
		m.lastClickAt = now
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

// ── Markdown formatting shortcuts ───────────────────────────────────────────

// toggleWrap wraps the cursor position in a pair of markers (e.g. "**" for
// bold, "_" for italic), leaving the cursor between them so the next
// keystrokes land inside the pair. If the cursor already sits directly
// between an empty pair of the same marker, the pair is removed instead —
// a quick way to undo an accidental press. This is the empty-span path used
// when there's no active word highlight to wrap instead — see wrapSelection
// for that case.
func toggleWrap(text string, cursorPos *int, marker string) string {
	runes := []rune(text)
	pos := *cursorPos
	if pos > len(runes) {
		pos = len(runes)
	}
	mk := []rune(marker)
	ml := len(mk)

	if pos >= ml && pos+ml <= len(runes) &&
		string(runes[pos-ml:pos]) == marker &&
		string(runes[pos:pos+ml]) == marker {
		runes = append(runes[:pos-ml], runes[pos+ml:]...)
		*cursorPos = pos - ml
		return string(runes)
	}

	out := make([]rune, 0, len(runes)+2*ml)
	out = append(out, runes[:pos]...)
	out = append(out, mk...)
	out = append(out, mk...)
	out = append(out, runes[pos:]...)
	*cursorPos = pos + ml
	return string(out)
}

// wrapSelection wraps the [start, end) rune span of text in a pair of
// markers (e.g. "**" for bold, "_" for italic) — the double-click-highlight
// counterpart to toggleWrap's empty-span behavior. It returns the modified
// text and a cursor position placed right after the closing marker.
func wrapSelection(text string, start, end int, marker string) (string, int) {
	runes := []rune(text)
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start >= end {
		return text, end
	}

	mk := []rune(marker)
	out := make([]rune, 0, len(runes)+2*len(mk))
	out = append(out, runes[:start]...)
	out = append(out, mk...)
	out = append(out, runes[start:end]...)
	out = append(out, mk...)
	out = append(out, runes[end:]...)
	return string(out), end + 2*len(mk)
}

// headingPrefixes maps heading level (0 = plain line) to its Markdown prefix.
var headingPrefixes = []string{"", "# ", "## ", "### "}

// cycleHeading rewrites the line the cursor is on, advancing it through
// plain → H1 → H2 → H3 → plain on each call. The cursor is kept at the same
// offset within the line content, shifted by the change in prefix length.
func cycleHeading(text string, cursorPos *int) string {
	runes := []rune(text)
	pos := *cursorPos
	if pos > len(runes) {
		pos = len(runes)
	}

	lineStart := pos
	for lineStart > 0 && runes[lineStart-1] != '\n' {
		lineStart--
	}
	lineEnd := lineStart
	for lineEnd < len(runes) && runes[lineEnd] != '\n' {
		lineEnd++
	}
	line := string(runes[lineStart:lineEnd])

	curLevel := 0
	for lvl := 3; lvl >= 1; lvl-- {
		if strings.HasPrefix(line, headingPrefixes[lvl]) {
			curLevel = lvl
			break
		}
	}
	nextLevel := (curLevel + 1) % len(headingPrefixes)
	oldPrefix := headingPrefixes[curLevel]
	newPrefix := headingPrefixes[nextLevel]
	newLine := newPrefix + strings.TrimPrefix(line, oldPrefix)

	newRunes := make([]rune, 0, len(runes)-len(oldPrefix)+len(newPrefix))
	newRunes = append(newRunes, runes[:lineStart]...)
	newRunes = append(newRunes, []rune(newLine)...)
	newRunes = append(newRunes, runes[lineEnd:]...)

	delta := len(newPrefix) - len(oldPrefix)
	newPos := pos + delta
	if minPos := lineStart + len(newPrefix); newPos < minPos {
		newPos = minPos
	}
	*cursorPos = newPos
	return string(newRunes)
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
	if m.mode == modeChangelogLatest || m.mode == modeChangelogHistory {
		dialog := m.renderChangelog(m.mode == modeChangelogLatest)
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
	case modeChangelogHistory:
		modeStr = th.StatusKey.Render(" CHANGELOG")
	case modeChangelogLatest:
		modeStr = th.StatusKey.Render(" WHAT'S NEW")
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

	// wrapW must match handleEditorClick's notion of the content area
	// exactly (same border + padding math) — editorRows, and the wrapping
	// done here, are the two halves of a single layout that render and
	// mouse-click handling both have to agree on.
	wrapW := w - 2
	if wrapW < 1 {
		wrapW = 1
	}
	fieldW, bodyW := editorFieldWidths(wrapW)

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

	// renderRow renders one visual row of a field: its text, plus the
	// cursor (if the field is active and it falls on this row) and any
	// active word-selection highlight that overlaps this row.
	renderRow := func(rowText string, field, rowStart int) string {
		runes := []rune(rowText)
		n := len(runes)

		cursorAt := -1
		if m.editField == field {
			cursorAt = m.editCursorPos - rowStart
		}

		selA, selB := -1, -1
		if m.selField == field {
			a, b := m.selStart-rowStart, m.selEnd-rowStart
			if a < 0 {
				a = 0
			}
			if b > n {
				b = n
			}
			if a < b {
				selA, selB = a, b
			}
		}

		var out strings.Builder
		for i := 0; i <= n; i++ {
			if i == n {
				if cursorAt == n {
					out.WriteString(th.Cursor.Render(" "))
				}
				break
			}
			switch {
			case cursorAt == i:
				out.WriteString(th.Cursor.Render(string(runes[i])))
			case selA >= 0 && i >= selA && i < selB:
				out.WriteString(th.Selection.Render(string(runes[i])))
			default:
				out.WriteRune(runes[i])
			}
		}
		return out.String()
	}

	// renderField soft-wraps text for the given field and renders each
	// resulting row, or a styled placeholder when the field is empty and
	// not focused.
	renderField := func(text string, field int, placeholder string) []string {
		if text == "" && m.editField != field {
			return []string{th.DetailMeta.Render(placeholder)}
		}
		fw := bodyW
		if field != 1 {
			fw = fieldW
		}
		lines, starts := wrapWithOffsets(text, fw)
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = renderRow(l, field, starts[i])
		}
		return out
	}

	titleRows := renderField(m.editTitle, 0, "(title)")
	bodyRows := renderField(m.editBody, 1, "(body)")
	tagsRows := renderField(m.editTags, 2, "(comma-separated, e.g. work, ideas)")

	// Continuation rows of a wrapped title/tags field have no label of
	// their own — pad them to the same column the label pushes row 0 to.
	blankLabel := strings.Repeat(" ", labelPrefixW)
	titleRows[0] = lipgloss.JoinHorizontal(lipgloss.Top, labelTitle, ": ", titleRows[0])
	for i := 1; i < len(titleRows); i++ {
		titleRows[i] = blankLabel + titleRows[i]
	}
	tagsRows[0] = lipgloss.JoinHorizontal(lipgloss.Top, labelTags, ": ", tagsRows[0])
	for i := 1; i < len(tagsRows); i++ {
		tagsRows[i] = blankLabel + tagsRows[i]
	}

	sep := th.DetailMeta.Render(strings.Repeat("─", wrapW))
	hint := th.DetailMeta.Render("tab: next field  ·  ctrl+b: bold  ·  ctrl+u: italic  ·  ctrl+t: heading  ·  ctrl+s: save  ·  ctrl+w: save & view  ·  esc: cancel")

	lines := make([]string, 0, len(titleRows)+len(bodyRows)+len(tagsRows)+6)
	lines = append(lines, titleRows...)
	lines = append(lines, sep)
	lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, labelBody, ": "))
	lines = append(lines, bodyRows...)
	lines = append(lines, sep)
	lines = append(lines, tagsRows...)
	lines = append(lines, "", hint)

	content := strings.Join(lines, "\n")

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
			th.StatusKey.Render("ctrl+b") + " bold",
			th.StatusKey.Render("ctrl+u") + " italic",
			th.StatusKey.Render("ctrl+t") + " heading",
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
			{"Ctrl+B", "Bold (body only)"},
			{"Ctrl+U", "Italic (body only)"},
			{"Ctrl+T", "Cycle heading H1 → H2 → H3 (body only)"},
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

	about := "nnn is a keyboard-driven TUI for managing notes in the terminal — built with Bubble Tea and lipgloss, because Markdown and fast navigation are all you really need."
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
		wasLatest := m.mode == modeChangelogLatest
		m.mode = modeList
		m.changelogOffset = 0
		// Only persist LastSeenVersion when dismissing the "What's New" overlay
		// (shown once on first launch after an upgrade). The history view must
		// not update it, or the user would never see the overlay again after
		// closing the history.
		if wasLatest {
			ver := strings.TrimPrefix(m.version, "v")
			return m, func() tea.Msg { return saveChangelogSeenMsg{version: ver} }
		}
		return m, nil
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

func (m Model) renderChangelog(whatsNew bool) string {
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

	// Build a working copy of the entries to avoid mutating the global slice.
	// The first entry always represents the current build; substitute the actual
	// running version so changelog.go never needs to be kept in sync with ldflags.
	// When showing "What's New" only the latest entry is included.
	var entries []ChangeLogEntry
	if whatsNew {
		first := changelogEntries[0]
		first.Version = ver
		entries = []ChangeLogEntry{first}
	} else {
		entries = make([]ChangeLogEntry, len(changelogEntries))
		copy(entries, changelogEntries)
		entries[0].Version = ver // inject running version for the first entry here too
	}

	var rows []string
	for _, entry := range entries {
		// Section header + blank line separator.
		rows = append(rows, th.HelpTitle.Render("What's new in v"+strings.TrimPrefix(entry.Version, "v")))
		rows = append(rows, "")
		for _, change := range entry.Description {
			wrapW := innerW - 3
			if wrapW < 10 {
				wrapW = 10
			}
			words := strings.Fields(change)
			line := ""
			firstWord := true // reset per-entry so each bullet gets its own "·"
			for _, word := range words {
				if line == "" {
					line = word
				} else if utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= wrapW {
					line += " " + word
				} else {
					prefix := "   "
					if firstWord {
						prefix = th.StatusKey.Render("·") + "  "
						firstWord = false
					}
					rows = append(rows, prefix+th.HelpDesc.Render(line))
					line = word
				}
			}
			if line != "" {
				prefix := "   "
				if firstWord {
					prefix = th.StatusKey.Render("·") + "  "
				}
				rows = append(rows, prefix+th.HelpDesc.Render(line))
			}
			rows = append(rows, "")
		}
	}

	footer := th.DetailMeta.Render("esc / q / enter: close  ·  j/k: scroll  ·  V: reopen anytime")

	totalRows := len(rows)

	// Reserve 1 line for the pinned footer and 2 for the border overhead.
	const overhead = 2
	visible := dialogH - overhead - 1 // -1 for pinned footer
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

	// Append the pinned footer after the scrollable slice so it always sits
	// flush at the bottom of the dialog regardless of scroll position.
	slice = append(slice, footer)

	content := strings.Join(slice, "\n")
	return th.HelpOverlay.Width(dialogW - 2).Height(dialogH - 2).Render(content)
}
