package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/antoniocali/nnn/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
)

// click sends a left mouse press at (x, y) through the real Update loop,
// exactly like a live terminal session would deliver it.
func click(t *testing.T, m Model, x, y int) Model {
	t.Helper()
	newModel, _ := m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m2, ok := newModel.(Model)
	if !ok {
		t.Fatalf("Update did not return a tui.Model")
	}
	return m2
}

// newEditorModel returns a Model sitting in the "new note" editor with a
// fixed 40-column width, which works out to listW=22, detailW=15, wrapW=13,
// fieldW=5 (title/tags) and bodyW=12 (body) — small enough to hand-compute
// wrap points for the tests below.
func newEditorModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	store, err := storage.New()
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	m, err := New(store, "amber", "dev")
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	m = press(t, m, keyRunes("n"))
	m.width = 40
	m.height = 24
	return m
}

// TestHandleEditorClickBodyWrappedLine is the regression test for the
// reported "click doesn't land in the right cell" bug: once a body line is
// long enough to soft-wrap onto a second screen row, clicking on that
// second row must resolve to the correct character in the *logical* body
// text, not just "row 2 of the raw '\n'-split lines" (which is what the
// original click math assumed).
func TestHandleEditorClickBodyWrappedLine(t *testing.T) {
	m := newEditorModel(t)
	m.editBody = "hello world this is nnn"

	// wrapWithOffsets("hello world this is nnn", 12) wraps to
	// ["hello world", "this is nnn"] starting at runes 0 and 12.
	// Row layout: 0=title, 1=sep, 2=body-label, 3=body row0, 4=body row1.
	// contentOriginX = listW+3 = 25, contentOriginY = 2.
	const contentOriginX, contentOriginY = 25, 2

	// Click the 'i' of the second "is" on the wrapped row — column 5 of
	// "this is nnn", which is absolute rune offset 12+5=17 in editBody.
	m2 := click(t, m, contentOriginX+5, contentOriginY+4)
	if m2.editField != 1 {
		t.Fatalf("editField = %d, want 1 (body)", m2.editField)
	}
	if m2.editCursorPos != 17 {
		t.Fatalf("editCursorPos = %d, want 17", m2.editCursorPos)
	}

	// Sanity-check the first (unwrapped) body row still resolves correctly.
	m3 := click(t, m, contentOriginX+3, contentOriginY+3)
	if m3.editField != 1 || m3.editCursorPos != 3 {
		t.Fatalf("row0 click -> field=%d pos=%d, want field=1 pos=3", m3.editField, m3.editCursorPos)
	}

	// Clicking past the end of a wrapped row clamps to that row's length.
	m4 := click(t, m, contentOriginX+50, contentOriginY+3)
	if m4.editField != 1 || m4.editCursorPos != 11 {
		t.Fatalf("clamped click -> field=%d pos=%d, want field=1 pos=11", m4.editField, m4.editCursorPos)
	}
}

// TestHandleEditorClickTitleField exercises the "Title: " label offset —
// title/tags rows are drawn labelPrefixW columns in from the body's rows,
// and the click math has to subtract that back out.
func TestHandleEditorClickTitleField(t *testing.T) {
	m := newEditorModel(t)
	m.editTitle = "hello"

	const contentOriginX, contentOriginY = 25, 2
	// "Title: " occupies labelPrefixW(7) columns before the text starts.
	m2 := click(t, m, contentOriginX+labelPrefixW+2, contentOriginY+0)
	if m2.editField != 0 {
		t.Fatalf("editField = %d, want 0 (title)", m2.editField)
	}
	if m2.editCursorPos != 2 {
		t.Fatalf("editCursorPos = %d, want 2", m2.editCursorPos)
	}
}

// TestDoubleClickSelectsWord checks that two presses on the same character
// within the double-click window highlight the whole word under it, and
// that a third press starts a fresh single click rather than immediately
// re-triggering.
func TestDoubleClickSelectsWord(t *testing.T) {
	m := newEditorModel(t)
	m.editBody = "hello world"

	const contentOriginX, contentOriginY = 25, 2
	// Row layout for this body (fits on one row, bodyW=12): body row0 is
	// content row index 3. Click the 'r' in "world" — rune offset 8.
	x, y := contentOriginX+8, contentOriginY+3

	m1 := click(t, m, x, y)
	if m1.selField != -1 {
		t.Fatalf("selField after single click = %d, want -1 (no selection)", m1.selField)
	}
	if m1.editCursorPos != 8 {
		t.Fatalf("editCursorPos after single click = %d, want 8", m1.editCursorPos)
	}

	m2 := click(t, m1, x, y)
	if m2.selField != 1 || m2.selStart != 6 || m2.selEnd != 11 {
		t.Fatalf("selection after double click = field %d [%d,%d), want field 1 [6,11) (\"world\")",
			m2.selField, m2.selStart, m2.selEnd)
	}
	if m2.editCursorPos != 11 {
		t.Fatalf("editCursorPos after double click = %d, want 11 (end of selection)", m2.editCursorPos)
	}

	// A third press at the same spot must not chain into another
	// double-click — it starts a fresh single click and drops the selection.
	m3 := click(t, m2, x, y)
	if m3.selField != -1 {
		t.Fatalf("selField after third click = %d, want -1", m3.selField)
	}
	if m3.editCursorPos != 8 {
		t.Fatalf("editCursorPos after third click = %d, want 8", m3.editCursorPos)
	}
}

// TestDoubleClickOutsideThresholdDoesNotSelect ensures two clicks on the
// same character that are too far apart in time are treated as two
// unrelated single clicks.
func TestDoubleClickOutsideThresholdDoesNotSelect(t *testing.T) {
	m := newEditorModel(t)
	m.editBody = "hello world"

	const contentOriginX, contentOriginY = 25, 2
	x, y := contentOriginX+8, contentOriginY+3

	m1 := click(t, m, x, y)
	m1.lastClickAt = m1.lastClickAt.Add(-2 * doubleClickWithin)

	m2 := click(t, m1, x, y)
	if m2.selField != -1 {
		t.Fatalf("selField = %d, want -1 (double-click window expired)", m2.selField)
	}
}

// TestDoubleClickOnWhitespaceSelectsNothing checks that double-clicking a
// position with no word character on either side leaves the cursor moved
// but selects nothing.
func TestDoubleClickOnWhitespaceSelectsNothing(t *testing.T) {
	m := newEditorModel(t)
	m.editBody = "   "

	const contentOriginX, contentOriginY = 25, 2
	x, y := contentOriginX+1, contentOriginY+3

	m1 := click(t, m, x, y)
	m2 := click(t, m1, x, y)
	if m2.selField != -1 {
		t.Fatalf("selField = %d, want -1 (nothing but whitespace to select)", m2.selField)
	}
	if m2.editCursorPos != 1 {
		t.Fatalf("editCursorPos = %d, want 1", m2.editCursorPos)
	}
}

// TestKeypressClearsSelection confirms a stale word highlight from a
// double-click doesn't linger once the user starts typing or navigating —
// this editor has no click-and-type replace-selection behavior.
func TestKeypressClearsSelection(t *testing.T) {
	m := newEditorModel(t)
	m.editField = 1
	m.selField = 1
	m.selStart = 0
	m.selEnd = 3

	m2 := press(t, m, keyType(tea.KeyRight))
	if m2.selField != -1 {
		t.Fatalf("selField after keypress = %d, want -1", m2.selField)
	}
}

// TestDoubleClickThenBoldWrapsTheHighlightedWord drives a real double-click
// followed by Ctrl+B through Update, end to end: once a word is highlighted,
// the bold/italic shortcuts should wrap that word instead of falling back to
// their empty-span behavior.
func TestDoubleClickThenBoldWrapsTheHighlightedWord(t *testing.T) {
	m := newEditorModel(t)
	m.editBody = "hello world"

	const contentOriginX, contentOriginY = 25, 2
	x, y := contentOriginX+8, contentOriginY+3 // the 'r' in "world"

	m = click(t, m, x, y)
	m = click(t, m, x, y)
	if m.selField != 1 || m.selStart != 6 || m.selEnd != 11 {
		t.Fatalf("selection before ctrl+b = field %d [%d,%d), want field 1 [6,11)", m.selField, m.selStart, m.selEnd)
	}

	m = press(t, m, keyType(tea.KeyCtrlB))
	if want := "hello **world**"; m.editBody != want {
		t.Fatalf("editBody after ctrl+b = %q, want %q", m.editBody, want)
	}
	if m.selField != -1 {
		t.Fatalf("selField after ctrl+b = %d, want -1 (selection consumed)", m.selField)
	}
	if want := utf8.RuneCountInString("hello **world**"); m.editCursorPos != want {
		t.Fatalf("editCursorPos after ctrl+b = %d, want %d (right after the closing **)", m.editCursorPos, want)
	}
}

// TestBoldWithNoSelectionStillInsertsEmptyPair confirms the pre-existing
// cursor-only behavior is untouched when there's nothing highlighted.
func TestBoldWithNoSelectionStillInsertsEmptyPair(t *testing.T) {
	m := newEditorModel(t)
	m.editField = 1
	m.editBody = ""
	m.editCursorPos = 0

	m = press(t, m, keyType(tea.KeyCtrlB))
	if m.editBody != "****" {
		t.Fatalf("editBody after ctrl+b = %q, want %q", m.editBody, "****")
	}
	if m.editCursorPos != 2 {
		t.Fatalf("editCursorPos after ctrl+b = %d, want 2", m.editCursorPos)
	}
}

// TestEditorStatusBarShowsFormattingShortcuts confirms the bottom status
// bar surfaces the bold/italic/heading shortcuts while editing, not just
// the in-panel hint line — they're easy to miss otherwise since they only
// apply to the body field.
func TestEditorStatusBarShowsFormattingShortcuts(t *testing.T) {
	m := newEditorModel(t)

	got := m.renderStatus()
	for _, want := range []string{"ctrl+b", "bold", "ctrl+u", "italic", "ctrl+t", "heading"} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderStatus() = %q, want it to contain %q", got, want)
		}
	}
}

func TestWrapWithOffsets(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		width      int
		wantLines  []string
		wantStarts []int
	}{
		{"empty", "", 10, []string{""}, []int{0}},
		{"fits on one line", "hello", 10, []string{"hello"}, []int{0}},
		{"exact width", "hello world", 11, []string{"hello world"}, []int{0}},
		{
			"wraps at a space", "hello world this is nnn", 12,
			[]string{"hello world", "this is nnn"}, []int{0, 12},
		},
		{
			"hard-breaks a word longer than width", "abcdefghijklmnop", 12,
			[]string{"abcdefghijkl", "mnop"}, []int{0, 12},
		},
		{
			"preserves explicit newlines", "foo\nbar", 12,
			[]string{"foo", "bar"}, []int{0, 4},
		},
		{
			"trailing newline yields a final empty line", "foo\n", 12,
			[]string{"foo", ""}, []int{0, 4},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines, starts := wrapWithOffsets(tt.text, tt.width)
			if len(lines) != len(tt.wantLines) {
				t.Fatalf("lines = %q, want %q", lines, tt.wantLines)
			}
			for i := range lines {
				if lines[i] != tt.wantLines[i] || starts[i] != tt.wantStarts[i] {
					t.Fatalf("line %d = %q@%d, want %q@%d", i, lines[i], starts[i], tt.wantLines[i], tt.wantStarts[i])
				}
			}
			for _, l := range lines {
				if n := len([]rune(l)); n > tt.width {
					t.Fatalf("line %q is %d runes, exceeds width %d", l, n, tt.width)
				}
			}
		})
	}
}

func TestWordBoundsAt(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		pos          int
		wantS, wantE int
	}{
		{"middle of word", "hello world", 8, 6, 11},
		{"start of word", "hello world", 6, 6, 11},
		{"cursor on space, word before", "hello world", 5, 0, 5},
		{"all whitespace", "   ", 1, 1, 1},
		{"empty text", "", 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, e := wordBoundsAt(tt.text, tt.pos)
			if s != tt.wantS || e != tt.wantE {
				t.Fatalf("wordBoundsAt(%q, %d) = (%d,%d), want (%d,%d)", tt.text, tt.pos, s, e, tt.wantS, tt.wantE)
			}
		})
	}
}

func TestEditorFieldWidths(t *testing.T) {
	tests := []struct {
		wrapW       int
		wantLabeled int
		wantBody    int
	}{
		{13, 5, 12},
		{100, 92, 99},
		{1, 1, 1},  // clamped
		{0, 1, 1},  // clamped
		{-5, 1, 1}, // clamped
	}
	for _, tt := range tests {
		labeled, body := editorFieldWidths(tt.wrapW)
		if labeled != tt.wantLabeled || body != tt.wantBody {
			t.Fatalf("editorFieldWidths(%d) = (%d,%d), want (%d,%d)", tt.wrapW, labeled, body, tt.wantLabeled, tt.wantBody)
		}
	}

	// The body reserve guarantees a maximally-wide row plus a trailing
	// cursor block never exceeds wrapW — the exact bug this all fixes.
	for _, wrapW := range []int{9, 10, 13, 40, 100} {
		_, body := editorFieldWidths(wrapW)
		if body+1 > wrapW {
			t.Fatalf("editorFieldWidths(%d): bodyW(%d)+cursor > wrapW(%d)", wrapW, body, wrapW)
		}
	}
}
