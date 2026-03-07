package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/antoniocali/nnn/internal/notes"
	"github.com/antoniocali/nnn/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Modes ────────────────────────────────────────────────────────────────────

type mode int

const (
	modeList   mode = iota // navigating the list
	modeDetail             // reading detail (right panel focused)
	modeEdit               // editing an existing note
	modeNew                // creating a new note
	modeSearch             // typing a search query
	modeDelete             // confirm delete
	modeHelp               // help overlay
)

// ── Messages ─────────────────────────────────────────────────────────────────

type errMsg struct{ err error }
type savedMsg struct{}
type statusClearMsg struct{}

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

	// status bar
	statusMsg    string
	statusIsErr  bool
	statusTicker int
}

// New creates a fresh TUI model.
func New(store *storage.Store) (Model, error) {
	ns, err := store.Load()
	if err != nil {
		return Model{}, err
	}
	m := Model{
		store:         store,
		allNotes:      ns,
		filteredNotes: ns,
	}
	return m, nil
}

func (m Model) Init() tea.Cmd {
	return nil
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

	case savedMsg:
		m.statusMsg = "Note saved"
		m.statusIsErr = false
		return m, clearStatusAfter(2 * time.Second)

	case statusClearMsg:
		m.statusMsg = ""
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
			return m.reloadNotes()
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
			return m.reloadNotes()
		}

	case "?":
		m.mode = modeHelp
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
			id := m.filteredNotes[m.cursor].ID
			if err := m.store.Delete(id); err != nil {
				m.mode = modeList
				return m, sendErr(err)
			}
			m.mode = modeList
			m.statusMsg = "Note deleted"
			m.statusIsErr = false
			return m.reloadNotesWith(clearStatusAfter(2 * time.Second))
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
	var err error
	if m.mode == modeNew || m.editID == "" {
		_, err = m.store.Create(title, m.editBody, tags)
	} else {
		err = m.store.Update(m.editID, title, m.editBody, tags)
	}
	if err != nil {
		return m, sendErr(err)
	}
	m.mode = modeList
	return m.reloadNotesWith(func() tea.Msg { return savedMsg{} })
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

	// Help overlay shortcircuit
	if m.mode == modeHelp {
		return m.renderHelp()
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

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		body,
		status,
	)
}

func (m Model) renderHeader() string {
	logo := appHeaderStyle.Render("◆ nnn")
	desc := appVersionStyle.Render("note manager")
	sep := statusSepStyle.Render(" │ ")
	mode := ""
	switch m.mode {
	case modeEdit:
		mode = statusKeyStyle.Render(" EDIT")
	case modeNew:
		mode = statusKeyStyle.Render(" NEW")
	case modeSearch:
		mode = searchActiveStyle.Render(" SEARCH: " + m.searchQuery + "█")
	case modeDelete:
		mode = statusErrStyle.Render(" DELETE?")
	}
	return logo + sep + desc + mode
}

func (m Model) renderList(w, h int) string {
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
		listHeaderStyle.Render("Notes"),
		" ",
		listCountStyle.Render(countStr),
	)
	rows = append(rows, header)
	rows = append(rows, listCountStyle.Render(strings.Repeat("─", innerW)))

	if len(m.filteredNotes) == 0 {
		rows = append(rows, emptyStyle.Width(innerW).Render("no notes"))
	}

	for i, n := range m.filteredNotes {
		pin := listItemNormalMark.String()
		if n.Pinned {
			pin = listItemPinnedMark.String()
		}
		title := n.Title
		if title == "" {
			title = "(untitled)"
		}
		maxTitleW := innerW - 4
		if utf8.RuneCountInString(title) > maxTitleW {
			title = string([]rune(title)[:maxTitleW-1]) + "…"
		}
		date := n.UpdatedAt.Format("Jan 02")

		line := pin + title
		meta := listCountStyle.Render(date)

		// Pad line to fill width then add date
		lineRunes := utf8.RuneCountInString(line)
		dateRunes := utf8.RuneCountInString(date)
		pad := innerW - lineRunes - dateRunes
		if pad < 0 {
			pad = 0
		}
		fullLine := line + strings.Repeat(" ", pad) + meta

		if i == m.cursor {
			rows = append(rows, listItemSelectedStyle.Width(innerW).Render(fullLine))
		} else {
			rows = append(rows, listItemStyle.Width(innerW).Render(fullLine))
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
		return listPanelFocStyle.Width(w).Height(h).Render(content)
	}
	return listPanelStyle.Width(w).Height(h).Render(content)
}

func (m Model) renderDetail(w, h int) string {
	focused := m.mode == modeDetail

	innerW := w - 4

	if m.mode == modeEdit || m.mode == modeNew {
		return m.renderEditor(w, h)
	}

	if len(m.filteredNotes) == 0 {
		content := emptyStyle.Width(innerW).Height(h).
			Render("No notes yet.\nPress  n  to create one.")
		return detailPanelStyle.Width(w).Height(h).Render(content)
	}

	n := m.filteredNotes[m.cursor]

	var parts []string

	// Title
	titleText := n.Title
	if titleText == "" {
		titleText = "(untitled)"
	}
	parts = append(parts, detailTitleStyle.Width(innerW).Render(titleText))

	// Tags
	if len(n.Tags) > 0 {
		var tagChips []string
		for _, t := range n.Tags {
			tagChips = append(tagChips, tagStyle.Render("#"+t))
		}
		parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Top, tagChips...))
	}

	// Meta
	created := n.CreatedAt.Format("Jan 02, 2006 15:04")
	updated := n.UpdatedAt.Format("Jan 02, 2006 15:04")
	metaLine := detailMetaStyle.Render(fmt.Sprintf("created %s  ·  updated %s", created, updated))
	if n.Pinned {
		metaLine = lipgloss.JoinHorizontal(lipgloss.Top,
			listItemPinnedMark.String()+" ",
			metaLine,
		)
	}
	parts = append(parts, metaLine)
	parts = append(parts, detailMetaStyle.Render(strings.Repeat("─", innerW)))

	// Body
	body := n.Body
	if body == "" {
		body = "(empty)"
	}
	parts = append(parts, detailBodyStyle.Width(innerW).Render(body))

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
		return detailPanelFocStyle.Width(w).Height(h).Render(visible)
	}
	return detailPanelStyle.Width(w).Height(h).Render(visible)
}

func (m Model) renderEditor(w, h int) string {
	innerW := w - 4

	// ── helper: render a field with cursor if active ───────────────────────
	renderField := func(text string, fieldIdx int, placeholder string) string {
		runes := []rune(text)
		if m.editField != fieldIdx {
			s := string(runes)
			if s == "" {
				return detailMetaStyle.Render(placeholder)
			}
			return s
		}
		pos := m.editCursorPos
		if pos > len(runes) {
			pos = len(runes)
		}
		before := string(runes[:pos])
		if pos < len(runes) {
			cur := cursorStyle.Render(string(runes[pos]))
			after := string(runes[pos+1:])
			return before + cur + after
		}
		return before + cursorStyle.Render(" ")
	}

	// ── fields ────────────────────────────────────────────────────────────
	titleActive := m.editField == 0
	bodyActive := m.editField == 1
	tagsActive := m.editField == 2

	labelTitle := editorTitleLabelStyle.Render("Title")
	labelBody := editorBodyLabelStyle.Render("Body ")
	labelTags := editorBodyLabelStyle.Render("Tags ")
	if titleActive {
		labelTitle = editorTitleLabelStyle.Render("Title")
	}
	if bodyActive {
		labelBody = editorTitleLabelStyle.Render("Body ")
	}
	if tagsActive {
		labelTags = editorTitleLabelStyle.Render("Tags ")
	}

	titleStr := renderField(m.editTitle, 0, "(title)")
	bodyStr := renderField(m.editBody, 1, "(body)")
	tagsStr := renderField(m.editTags, 2, "(comma-separated, e.g. work, ideas)")

	sep := detailMetaStyle.Render(strings.Repeat("─", innerW))
	hint := detailMetaStyle.Render("tab: next field  ·  ctrl+s: save  ·  ctrl+w: save & view  ·  esc: cancel")

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

	return editorPanelStyle.Width(w).Height(h).Render(content)
}

func (m Model) renderStatus() string {
	if m.statusMsg != "" {
		if m.statusIsErr {
			return statusErrStyle.Render("✗ " + m.statusMsg)
		}
		return statusMsgStyle.Render("✓ " + m.statusMsg)
	}

	sep := statusSepStyle.Render(" · ")

	switch m.mode {
	case modeList:
		keys := []string{
			statusKeyStyle.Render("j/k") + " nav",
			statusKeyStyle.Render("n") + " new",
			statusKeyStyle.Render("e") + " edit",
			statusKeyStyle.Render("d") + " del",
			statusKeyStyle.Render("p") + " pin",
			statusKeyStyle.Render("/") + " search",
			statusKeyStyle.Render("?") + " help",
			statusKeyStyle.Render("q") + " quit",
		}
		return statusBarStyle.Render(strings.Join(keys, sep))
	case modeDetail:
		keys := []string{
			statusKeyStyle.Render("j/k") + " scroll",
			statusKeyStyle.Render("e") + " edit",
			statusKeyStyle.Render("d") + " del",
			statusKeyStyle.Render("esc/h") + " back",
		}
		return statusBarStyle.Render(strings.Join(keys, sep))
	case modeEdit, modeNew:
		fieldName := []string{"title", "body", "tags"}[m.editField]
		keys := []string{
			statusKeyStyle.Render("tab") + " next field",
			statusKeyStyle.Render("ctrl+s") + " save",
			statusKeyStyle.Render("esc") + " cancel",
			detailMetaStyle.Render("editing: ") + statusKeyStyle.Render(fieldName),
		}
		return statusBarStyle.Render(strings.Join(keys, sep))
	case modeSearch:
		return searchActiveStyle.Render("  Searching: " + m.searchQuery + "█  ·  enter/esc to confirm")
	case modeDelete:
		n := m.filteredNotes[m.cursor]
		title := n.Title
		if title == "" {
			title = "(untitled)"
		}
		return statusErrStyle.Render(fmt.Sprintf("  Delete \"%s\"? y/n", title))
	}
	return ""
}

func (m Model) renderHelp() string {
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
			{"?", "Toggle help"},
			{"q / Ctrl+C", "Quit"},
		}},
	}

	// Build all content lines (unstyled box, just the inner rows)
	var rows []string
	rows = append(rows, helpTitleStyle.Render("◆ nnn keyboard shortcuts"), "")
	for _, sec := range sections {
		rows = append(rows, helpTitleStyle.Render(sec.header))
		for _, b := range sec.bindings {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top,
				helpKeyStyle.Render(b.key),
				helpDescStyle.Render(b.desc),
			))
		}
		rows = append(rows, "")
	}
	rows = append(rows, detailMetaStyle.Render("j/k: scroll  ·  g/G: top/bottom  ·  esc/?/q: close"))

	totalRows := len(rows)

	// Overhead = 1 border top + 1 border bottom = 2 rows.
	// Padding is horizontal-only so no extra vertical rows.
	const helpOverhead = 2
	visible := m.height - helpOverhead
	if visible < 1 {
		visible = 1
	}

	// Clamp offset
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

	// Pad to fill the box height so the border stays stable
	for len(slice) < visible {
		slice = append(slice, "")
	}

	// Add a scroll indicator in the top-right corner of the title line when
	// the content is taller than the viewport.
	if totalRows > visible {
		indicator := detailMetaStyle.Render(fmt.Sprintf("%d%%", (off+1)*100/totalRows))
		titleRow := slice[0]
		pad := m.width - 6 - lipgloss.Width(titleRow) - lipgloss.Width(indicator)
		if pad > 0 {
			slice[0] = titleRow + strings.Repeat(" ", pad) + indicator
		}
	}

	content := strings.Join(slice, "\n")
	return helpOverlayStyle.Width(m.width - 2).Height(m.height - 2).Render(content)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
