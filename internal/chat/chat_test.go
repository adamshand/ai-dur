package chat

import "testing"

func TestRenderInputEmptyCursorAfterPrompt(t *testing.T) {
	lines, row, col := renderInput("adam", "", 0, 80)
	if len(lines) != 1 {
		t.Fatalf("len(lines) = %d, want 1", len(lines))
	}
	if row != 0 {
		t.Fatalf("row = %d, want 0", row)
	}
	if col != len("adam> ") {
		t.Fatalf("col = %d, want %d", col, len("adam> "))
	}
}

func TestRenderInputWrappedBoundaryCursorStartsNextLine(t *testing.T) {
	// Width 14 leaves 8 columns after the "adam> " prompt.
	_, row, col := renderInput("adam", "abcdefghZ", len("abcdefgh"), 14)
	if row != 1 {
		t.Fatalf("row = %d, want 1", row)
	}
	if col != len("adam> ") {
		t.Fatalf("col = %d, want %d", col, len("adam> "))
	}
}

func TestRenderInputWideRuneCursorUsesDisplayWidth(t *testing.T) {
	_, _, col := renderInput("adam", "界a", len("界"), 80)
	want := len("adam> ") + 2
	if col != want {
		t.Fatalf("col = %d, want %d", col, want)
	}
}

func TestBottomPadRowsAccountsForTranscript(t *testing.T) {
	if got := bottomPadRows(25, 3, 2); got != 20 {
		t.Fatalf("bottomPadRows = %d, want 20", got)
	}
	if got := bottomPadRows(5, 10, 2); got != 0 {
		t.Fatalf("bottomPadRows overflow = %d, want 0", got)
	}
}
