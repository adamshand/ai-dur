package chat

import (
	"strings"
	"testing"

	"github.com/adamshand/aidur/internal/tools"
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
	if col != 0 {
		t.Fatalf("col = %d, want 0", col)
	}
}

func TestRenderInputWideRuneCursorUsesDisplayWidth(t *testing.T) {
	_, _, col := renderInput("adam", "界a", len("界"), 80)
	want := len("adam> ") + 2
	if col != want {
		t.Fatalf("col = %d, want %d", col, want)
	}
}

func TestRenderMessageUsesHostnameAgentPrompt(t *testing.T) {
	lines := renderMessageWithAgentPrompt("agent", "hello", 80, "weka")
	if len(lines) != 1 {
		t.Fatalf("len(lines) = %d, want 1", len(lines))
	}
	if got := stripANSI(lines[0]); got != "weka> hello" {
		t.Fatalf("rendered agent line = %q, want %q", got, "weka> hello")
	}
}

func TestRenderWrappedMessageDoesNotIndentContinuationLines(t *testing.T) {
	lines := renderMessageWithAgentPrompt("agent", "abcdefghZ", 14, "weka")
	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2", len(lines))
	}
	if got := stripANSI(lines[0]); got != "weka> abcdefgh" {
		t.Fatalf("first line = %q, want %q", got, "weka> abcdefgh")
	}
	if got := stripANSI(lines[1]); got != "Z" {
		t.Fatalf("continuation line = %q, want %q", got, "Z")
	}
}

func TestRenderTranscriptItemRendersCollapsedAndExpandedTool(t *testing.T) {
	ui := &TerminalUI{}
	rec := tools.Record{ID: 7, Trace: "pwd", Result: "exit_code: 0\nstdout:\n/tmp\nstderr:\n", Elapsed: 0}

	collapsed := strings.Join(ui.renderTranscriptItemLocked(transcriptItem{Role: "tool", Tool: &rec}, 80), "\n")
	if got := stripANSI(collapsed); !strings.Contains(got, "[tool] 7 ✓ ▸ pwd exit 0") || strings.Contains(got, "stdout") {
		t.Fatalf("collapsed tool render = %q", got)
	}

	expanded := strings.Join(ui.renderTranscriptItemLocked(transcriptItem{Role: "tool", Tool: &rec, ToolExpanded: true}, 80), "\n")
	got := stripANSI(expanded)
	if !strings.Contains(got, "[tool] 7 ✓ ▾ pwd exit 0") || !strings.Contains(got, "stdout") || !strings.Contains(got, "/tmp") {
		t.Fatalf("expanded tool render = %q", got)
	}
}

func TestToggleToolTooFarBackAppendsExpandedFallback(t *testing.T) {
	oldTerminalSize := terminalSize
	terminalSize = func(int) (int, int, error) { return 80, 3, nil }
	defer func() { terminalSize = oldTerminalSize }()

	rec := tools.Record{ID: 7, Trace: "pwd", Result: "exit_code: 0\nstdout:\n/tmp\nstderr:\n"}
	ui := &TerminalUI{
		items:     []transcriptItem{{Role: "tool", Tool: &rec, PrintedRows: 1}},
		liveLines: 3,
	}

	if !ui.ToggleLastTool() {
		t.Fatalf("ToggleLastTool returned false")
	}
	if len(ui.items) != 2 {
		t.Fatalf("len(items) = %d, want fallback appended", len(ui.items))
	}
	if ui.items[0].ToolExpanded {
		t.Fatalf("original tool should remain collapsed when fallback is appended")
	}
	if ui.items[1].Tool == nil || !ui.items[1].ToolExpanded {
		t.Fatalf("fallback item = %#v, want expanded tool", ui.items[1])
	}
}

func TestStartupPromptSummaryShowsSystemPromptAndInstructions(t *testing.T) {
	got := startupPromptSummary("prefer examples")
	if !strings.Contains(got, "system prompt:\n") || !strings.Contains(got, "custom instructions:\nprefer examples") {
		t.Fatalf("startupPromptSummary missing prompt details: %q", got)
	}
}

func TestStartupPromptSummaryShowsNoInstructions(t *testing.T) {
	got := startupPromptSummary("   ")
	if !strings.Contains(got, "custom instructions:\nnone") {
		t.Fatalf("startupPromptSummary blank instructions = %q, want none", got)
	}
}

func TestChatPromptIncludesCustomInstructions(t *testing.T) {
	s := &Session{Instructions: "prefer examples"}
	got := s.chatPrompt()
	if !strings.Contains(got, "Additional user instructions:\nprefer examples") {
		t.Fatalf("chatPrompt missing custom instructions: %q", got)
	}
}

func TestInstructionsStatus(t *testing.T) {
	if got := instructionsStatus("   "); got != "none" {
		t.Fatalf("instructionsStatus blank = %q, want none", got)
	}
	if got := instructionsStatus("be brief"); got != "set" {
		t.Fatalf("instructionsStatus set = %q, want set", got)
	}
}

func TestTerminalInputModesDoNotQueryOrReportKeyRelease(t *testing.T) {
	if strings.Contains(enableTerminalInputModesSequence, "\x1b[?u") {
		t.Fatalf("enableTerminalInputModesSequence queries terminal support; query responses can leak after suspend")
	}
	if strings.Contains(enableTerminalInputModesSequence, "\x1b[>7u") {
		t.Fatalf("enableTerminalInputModesSequence enables key-release events")
	}
}

func TestParseKeyEventNavigationSequences(t *testing.T) {
	cases := []struct {
		seq string
		key Key
	}{
		{"\x1b[A", KeyUp},
		{"\x1b[B", KeyDown},
		{"\x1b[D", KeyLeft},
		{"\x1b[C", KeyRight},
		{"\x1b[1;1D", KeyLeft},
		{"\x1b[1;5C", KeyRight},
		{"\x1bOD", KeyLeft},
		{"\x1bOC", KeyRight},
		{"\x1b[H", KeyHome},
		{"\x1b[F", KeyEnd},
		{"\x1b[3~", KeyDelete},
	}
	for _, tc := range cases {
		n, event, ok := parseKeyEventPrefix(tc.seq + "rest")
		if !ok || n != len(tc.seq) || event.Key != tc.key {
			t.Fatalf("parseKeyEventPrefix(%q) = (%d, %#v, %v), want (%d, %s, true)", tc.seq, n, event, ok, len(tc.seq), tc.key)
		}
	}
}

func TestHandleDataSupportsModifiedArrowKeys(t *testing.T) {
	ui := &TerminalUI{}
	ui.handleData("abc")
	ui.handleData("\x1b[1;1D")
	ui.handleData("X")
	if ui.input != "abXc" {
		t.Fatalf("input after modified left arrow = %q, want abXc", ui.input)
	}
}

func TestHandleDataSupportsKittyFunctionalArrowKeys(t *testing.T) {
	ui := &TerminalUI{}
	ui.handleData("abc")
	ui.handleData("\x1b[57417u")
	ui.handleData("X")
	if ui.input != "abXc" {
		t.Fatalf("input after Kitty left arrow = %q, want abXc", ui.input)
	}
}

func TestHandleDataConsumesUpDownArrows(t *testing.T) {
	ui := &TerminalUI{}
	ui.handleData("abc")
	ui.handleData("\x1b[A")
	ui.handleData("\x1b[B")
	ui.handleData("\x1b[57416u")
	ui.handleData("\x1b[57419u")
	if ui.input != "abc" {
		t.Fatalf("input after up/down arrows = %q, want abc", ui.input)
	}
}

func TestHandleDataSupportsReadlineWordKeys(t *testing.T) {
	ui := &TerminalUI{}
	ui.handleData("one two")
	ui.handleData("\x1bb")
	ui.handleData("X")
	if ui.input != "one Xtwo" {
		t.Fatalf("input after alt-b = %q, want one Xtwo", ui.input)
	}
	ui.handleData("\x17")
	if ui.input != "one two" {
		t.Fatalf("input after ctrl-w = %q, want one two", ui.input)
	}
	ui.handleData("\x1bd")
	if ui.input != "one " {
		t.Fatalf("input after alt-d = %q, want one-space", ui.input)
	}
	ui.handleData("\x19")
	if ui.input != "one two" {
		t.Fatalf("input after ctrl-y = %q, want one two", ui.input)
	}
}

func TestHandleDataSupportsMacOptionWordKeys(t *testing.T) {
	ui := &TerminalUI{}
	ui.handleData("one two")
	ui.handleData("∫")
	ui.handleData("X")
	if ui.input != "one Xtwo" {
		t.Fatalf("input after option-b fallback = %q, want one Xtwo", ui.input)
	}
	ui.handleData("∂")
	if ui.input != "one X" {
		t.Fatalf("input after option-d fallback = %q, want one X", ui.input)
	}
}

func TestHandleDataSupportsInputHistory(t *testing.T) {
	ui := &TerminalUI{}
	ui.handleData("first\r")
	ui.handleData("second\r")
	ui.handleData("draft")
	ui.handleData("\x1b[A")
	if ui.input != "second" {
		t.Fatalf("up history = %q, want second", ui.input)
	}
	ui.handleData("\x1b[A")
	if ui.input != "first" {
		t.Fatalf("second up history = %q, want first", ui.input)
	}
	ui.handleData("\x1b[B")
	if ui.input != "second" {
		t.Fatalf("down history = %q, want second", ui.input)
	}
	ui.handleData("\x1b[B")
	if ui.input != "draft" {
		t.Fatalf("down restores draft = %q, want draft", ui.input)
	}
}

func TestHandleDataSupportsEmacsHistoryKeys(t *testing.T) {
	ui := &TerminalUI{}
	ui.handleData("first\r")
	ui.handleData("second\r")
	ui.handleData("\x10")
	if ui.input != "second" {
		t.Fatalf("ctrl-p history = %q, want second", ui.input)
	}
	ui.handleData("\x10")
	if ui.input != "first" {
		t.Fatalf("second ctrl-p history = %q, want first", ui.input)
	}
	ui.handleData("\x0e")
	if ui.input != "second" {
		t.Fatalf("ctrl-n history = %q, want second", ui.input)
	}
}

func TestCtrlDQuitsOnlyWhenInputEmpty(t *testing.T) {
	ui := &TerminalUI{running: true}
	ui.handleData("ab")
	ui.handleData("\x02")
	ui.handleData("\x04")
	if !ui.running || ui.input != "a" {
		t.Fatalf("ctrl-d with input running=%v input=%q, want still running and input a", ui.running, ui.input)
	}
	ui.handleData("\x15")
	ui.handleData("\x04")
	if ui.running {
		t.Fatalf("ctrl-d on empty should quit")
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

func TestParseKeyEventLowercasesCtrlLetters(t *testing.T) {
	_, event, ok := parseKeyEventPrefix("\x1b[65;5u")
	if !ok || event.Key != KeyRune || event.Rune != 'a' || !event.Mods.Ctrl {
		t.Fatalf("parseKeyEventPrefix ctrl-A = %#v, %v; want ctrl rune a", event, ok)
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

func TestLiveContentRowsLeavesRoomForFooter(t *testing.T) {
	if got := liveContentRows(10, true); got != 7 {
		t.Fatalf("liveContentRows with transcript = %d, want 7", got)
	}
	if got := liveContentRows(10, false); got != 8 {
		t.Fatalf("liveContentRows without transcript = %d, want 8", got)
	}
	if got := liveContentRows(1, true); got != 1 {
		t.Fatalf("liveContentRows tiny terminal = %d, want 1", got)
	}
}

func TestTrimLiveLinesShowsTailWithinLimit(t *testing.T) {
	lines := []string{"one", "two", "three", "four"}
	got := trimLiveLines(lines, 3)
	if len(got) != 3 {
		t.Fatalf("len(trimLiveLines) = %d, want 3", len(got))
	}
	if !strings.Contains(stripANSI(got[0]), "2 lines above") {
		t.Fatalf("trim indicator = %q, want omitted line count", got[0])
	}
	if got[1] != "three" || got[2] != "four" {
		t.Fatalf("trimmed tail = %#v, want three/four", got)
	}
}

func TestTrimLiveLinesOneRowShowsIndicator(t *testing.T) {
	got := trimLiveLines([]string{"one", "two"}, 1)
	if len(got) != 1 || !strings.Contains(stripANSI(got[0]), "2 lines above") {
		t.Fatalf("trimLiveLines one row = %#v, want indicator", got)
	}
}

func TestShortHostRemovesDomain(t *testing.T) {
	if got := shortHost("srv.example.com"); got != "srv" {
		t.Fatalf("shortHost = %q, want srv", got)
	}
	if got := shortHost(""); got != "host" {
		t.Fatalf("shortHost empty = %q, want host", got)
	}
}

func TestSanitizeDisplayTextStripsTerminalControls(t *testing.T) {
	input := "ok\x1b[31mred\x1b[0m\x1b]0;title\a\nnext\x07"
	got := sanitizeDisplayText(input)
	want := "okred\nnext"
	if got != want {
		t.Fatalf("sanitizeDisplayText = %q, want %q", got, want)
	}
}

func TestRenderMessageSanitizesUntrustedText(t *testing.T) {
	lines := renderMessageWithAgentPrompt("agent", "hi\x1b[2J", 80, "host")
	got := stripANSI(strings.Join(lines, "\n"))
	if strings.Contains(got, "\x1b") || got != "host> hi" {
		t.Fatalf("rendered sanitized message = %q, want host> hi", got)
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
