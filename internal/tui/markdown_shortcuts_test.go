package tui

import "testing"

func TestToggleWrapInsertsPairAroundCursor(t *testing.T) {
	pos := 5
	got := toggleWrap("Hello", &pos, "**")

	want := "Hello****"
	if got != want {
		t.Fatalf("toggleWrap() = %q, want %q", got, want)
	}
	if pos != 7 {
		t.Fatalf("cursorPos = %d, want 7", pos)
	}
}

func TestToggleWrapMidText(t *testing.T) {
	pos := 5 // "Hello| World"
	got := toggleWrap("Hello World", &pos, "_")

	want := "Hello__ World"
	if got != want {
		t.Fatalf("toggleWrap() = %q, want %q", got, want)
	}
	if pos != 6 {
		t.Fatalf("cursorPos = %d, want 6", pos)
	}
}

func TestToggleWrapRemovesEmptyPair(t *testing.T) {
	text := "Hello****"
	pos := 7 // cursor between the two "**" pairs

	got := toggleWrap(text, &pos, "**")

	want := "Hello"
	if got != want {
		t.Fatalf("toggleWrap() = %q, want %q", got, want)
	}
	if pos != 5 {
		t.Fatalf("cursorPos = %d, want 5", pos)
	}
}

func TestToggleWrapDoesNotRemoveNonEmptyPair(t *testing.T) {
	text := "**bold**"
	pos := 6 // cursor between "bold" and the closing "**"

	got := toggleWrap(text, &pos, "**")

	// Cursor isn't directly between an *empty* pair, so a new pair is inserted
	// rather than the existing (non-empty) one being removed.
	want := "**bold" + "****" + "**"
	if got != want {
		t.Fatalf("toggleWrap() = %q, want %q", got, want)
	}
}

func TestCycleHeadingPlainToH1ToH2ToH3ToPlain(t *testing.T) {
	pos := 5 // somewhere inside "Hello"
	line := "Hello"

	line = cycleHeading(line, &pos)
	if line != "# Hello" || pos != 7 {
		t.Fatalf("after 1st cycle: line=%q pos=%d, want %q pos=7", line, pos, "# Hello")
	}

	line = cycleHeading(line, &pos)
	if line != "## Hello" || pos != 8 {
		t.Fatalf("after 2nd cycle: line=%q pos=%d, want %q pos=8", line, pos, "## Hello")
	}

	line = cycleHeading(line, &pos)
	if line != "### Hello" || pos != 9 {
		t.Fatalf("after 3rd cycle: line=%q pos=%d, want %q pos=9", line, pos, "### Hello")
	}

	line = cycleHeading(line, &pos)
	if line != "Hello" || pos != 5 {
		t.Fatalf("after 4th cycle: line=%q pos=%d, want %q pos=5", line, pos, "Hello")
	}
}

func TestCycleHeadingOnlyAffectsCurrentLine(t *testing.T) {
	text := "First\nSecond\nThird"
	pos := 9 // inside "Second"

	got := cycleHeading(text, &pos)

	want := "First\n# Second\nThird"
	if got != want {
		t.Fatalf("cycleHeading() = %q, want %q", got, want)
	}
}

func TestCycleHeadingCursorClampedInsideNewPrefix(t *testing.T) {
	pos := 0 // start of line, before any prefix
	line := "Hello"

	got := cycleHeading(line, &pos)

	if got != "# Hello" {
		t.Fatalf("cycleHeading() = %q, want %q", got, "# Hello")
	}
	if pos != 2 {
		t.Fatalf("cursorPos = %d, want 2 (clamped to end of new prefix)", pos)
	}
}
