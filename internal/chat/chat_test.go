package chat

import (
	"strings"
	"testing"

	"github.com/adamshand/aidur/internal/provider"
)

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

func TestRenderMessageUsesConfiguredAgentName(t *testing.T) {
	lines := renderMessageWithAgentName("agent", "hello", 80, "edda")
	if len(lines) != 1 {
		t.Fatalf("len(lines) = %d, want 1", len(lines))
	}
	if got := stripANSI(lines[0]); got != "edda> hello" {
		t.Fatalf("rendered agent line = %q, want %q", got, "edda> hello")
	}
}

func TestChatPromptIncludesConfiguredAgentName(t *testing.T) {
	agentName := "edda"
	s := &Session{AgentName: agentName, AgentNameSource: "config"}
	got := s.chatPrompt()
	want := "Your name in this chat is " + agentName + "."
	if !strings.Contains(got, want) {
		t.Fatalf("chatPrompt missing agent name note: %q", got)
	}
}

func TestChatPromptOmitsDefaultAgentName(t *testing.T) {
	s := &Session{AgentName: "aidur", AgentNameSource: "default"}
	if got := s.chatPrompt(); got != provider.ChatPrompt {
		t.Fatalf("chatPrompt default = %q, want base chat prompt", got)
	}
}

func TestModifiedEnterSequenceLen(t *testing.T) {
	cases := []string{
		"\x1b[13;2u",
		"\x1b[13;2;1u",
		"\x1b[27;2;13~",
	}
	for _, tc := range cases {
		n, ok := modifiedEnterSequenceLen(tc + "rest")
		if !ok {
			t.Fatalf("modifiedEnterSequenceLen(%q) ok=false, want true", tc)
		}
		if n != len(tc) {
			t.Fatalf("modifiedEnterSequenceLen(%q) len=%d, want %d", tc, n, len(tc))
		}
	}
	if _, ok := modifiedEnterSequenceLen("\x1b[13;1u"); ok {
		t.Fatalf("plain CSI-u enter matched as modified enter")
	}
}

func TestParseTerminalKeySequence(t *testing.T) {
	cases := []struct {
		seq  string
		code rune
		mod  int
	}{
		{"\x1b[13u", 13, 1},
		{"\x1b[13;5u", 13, 5},
		{"\x1b[13;9u", 13, 9},
		{"\x1b[27u", 27, 1},
		{"\x1b[100;5u", 'd', 5},
		{"\x1b[122;5u", 'z', 5},
		{"\x1b[27;5;100~", 'd', 5},
	}
	for _, tc := range cases {
		n, code, mod, ok := parseTerminalKeySequence(tc.seq + "rest")
		if !ok {
			t.Fatalf("parseTerminalKeySequence(%q) ok=false, want true", tc.seq)
		}
		if n != len(tc.seq) || code != tc.code || mod != tc.mod {
			t.Fatalf("parseTerminalKeySequence(%q) = (%d,%q,%d), want (%d,%q,%d)", tc.seq, n, code, mod, len(tc.seq), tc.code, tc.mod)
		}
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

func TestRenderStatusBarPadsToFullWidth(t *testing.T) {
	bar := renderStatusBar("abc", 8)
	plain := stripANSI(bar)
	if plain != "abc     " {
		t.Fatalf("plain status bar = %q, want %q", plain, "abc     ")
	}
	if visibleWidthNoANSI(bar) != 8 {
		t.Fatalf("visible width = %d, want 8", visibleWidthNoANSI(bar))
	}
}

func TestStatusBarStyle(t *testing.T) {
	if got := statusBarStyle(501, false, false); got != statusBarNormal {
		t.Fatalf("normal style = %q, want %q", got, statusBarNormal)
	}
	if got := statusBarStyle(501, false, true); got != statusBarSSH {
		t.Fatalf("ssh style = %q, want %q", got, statusBarSSH)
	}
	if got := statusBarStyle(0, false, true); got != statusBarRoot {
		t.Fatalf("root+ssh style = %q, want root %q", got, statusBarRoot)
	}
	if got := statusBarStyle(501, true, true); got != statusBarRoot {
		t.Fatalf("sudo+ssh style = %q, want root %q", got, statusBarRoot)
	}
}

func TestUserPromptColor(t *testing.T) {
	if got := userPromptColor(501, false, false); got != white {
		t.Fatalf("normal prompt color = %q, want %q", got, white)
	}
	if got := userPromptColor(501, false, true); got != yellow {
		t.Fatalf("ssh prompt color = %q, want %q", got, yellow)
	}
	if got := userPromptColor(0, false, true); got != red {
		t.Fatalf("root+ssh prompt color = %q, want %q", got, red)
	}
	if got := userPromptColor(501, true, true); got != red {
		t.Fatalf("sudo+ssh prompt color = %q, want %q", got, red)
	}
}
