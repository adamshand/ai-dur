package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"

	"github.com/adamshand/aidur/internal/config"
	"github.com/adamshand/aidur/internal/provider"
	"github.com/adamshand/aidur/internal/tools"
	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

const (
	red   = "\033[31m"
	green = "\033[32m"
	blue  = "\033[34m"
	dim   = "\033[2m"
	reset = "\033[0m"
)

type Session struct {
	Provider   provider.Client
	Cfg        config.Config
	Model      string
	Thinking   string
	Debug      bool
	Runner     *tools.Runner
	History    []map[string]any
	UI         *TerminalUI
	turnMu     sync.Mutex
	turnID     uint64
	turnCancel context.CancelFunc
}

type transcriptItem struct {
	Role string
	Text string
}

type TerminalUI struct {
	mu          sync.Mutex
	items       []transcriptItem
	input       string
	cursor      int
	oldState    *term.State
	pasteMode   bool
	pasteBuf    strings.Builder
	liveLines   int
	cursorRow   int
	running     bool
	statusFunc  func() string
	onSubmit    func(string)
	onCancel    func()
	deltaActive bool
	deltaRole   string
}

func Run(debug bool) int {
	cfg := config.Load()
	model, _ := config.EffectiveModel(cfg)
	thinking, _ := config.EffectiveThinking(cfg)
	cwd, _ := os.Getwd()
	s := &Session{Provider: provider.New(), Cfg: cfg, Model: model, Thinking: thinking, Debug: debug, Runner: tools.NewRunner(cwd)}
	ui := &TerminalUI{}
	s.UI = ui
	ui.statusFunc = s.statusLine
	ui.onCancel = func() { s.cancelTurn() }
	ui.onSubmit = func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if strings.HasPrefix(text, "/") {
			if s.command(text) {
				ui.running = false
			}
			return
		}
		ui.Append(userName(), text)
		s.History = append(s.History, map[string]any{"role": "user", "content": text})
		go func() {
			if err := s.turn(); err != nil {
				if errors.Is(err, context.Canceled) {
					ui.Append("dur", "cancelled")
					return
				}
				ui.Append("dur", err.Error())
			}
		}()
	}
	ui.items = append(ui.items, transcriptItem{Role: "dur", Text: "ephemeral session; read-only tools enabled\nEnter sends. Bracketed paste inserts normally. Shift-Enter newline if supported. Ctrl-C clears. Ctrl-D exits. Ctrl-Z suspends. /help for commands."})
	if err := ui.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "dur:", err)
		return 1
	}
	return 0
}

func (ui *TerminalUI) Run() error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return fmt.Errorf("chat requires a terminal")
	}
	old, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	ui.oldState = old
	ui.running = true
	defer ui.restore()
	fmt.Print("\033[?2004h")
	defer fmt.Print("\033[?2004l\033[?25h")
	ui.mu.Lock()
	_, h, sizeErr := term.GetSize(int(os.Stdout.Fd()))
	if sizeErr != nil || h <= 0 {
		h = 24
	}
	transcriptRows := 0
	for _, item := range ui.items {
		transcriptRows += ui.printMessageLocked(item.Role, item.Text)
	}
	ui.padToBottomLocked(h, transcriptRows)
	ui.renderLiveLocked()
	ui.mu.Unlock()
	buf := make([]byte, 4096)
	for ui.running {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		ui.handleData(string(buf[:n]))
		ui.renderLive()
	}
	return nil
}

func (ui *TerminalUI) restore() {
	if ui.oldState != nil {
		_ = term.Restore(int(os.Stdin.Fd()), ui.oldState)
	}
}

func (ui *TerminalUI) handleData(data string) {
	for data != "" {
		if ui.pasteMode {
			if idx := strings.Index(data, "\x1b[201~"); idx >= 0 {
				ui.pasteBuf.WriteString(data[:idx])
				ui.insertText(normalizePaste(ui.pasteBuf.String()))
				ui.pasteBuf.Reset()
				ui.pasteMode = false
				data = data[idx+6:]
				continue
			}
			ui.pasteBuf.WriteString(data)
			return
		}
		if strings.HasPrefix(data, "\x1b[200~") {
			ui.pasteMode = true
			ui.pasteBuf.Reset()
			data = data[6:]
			continue
		}
		if data == "\x1b" {
			ui.cancel()
			return
		}
		if strings.HasPrefix(data, "\x1b[D") {
			ui.moveLeft()
			data = data[3:]
			continue
		}
		if strings.HasPrefix(data, "\x1b[C") {
			ui.moveRight()
			data = data[3:]
			continue
		}
		if strings.HasPrefix(data, "\x1b[H") || strings.HasPrefix(data, "\x1b[1~") {
			ui.moveStart()
			data = dropEscape(data)
			continue
		}
		if strings.HasPrefix(data, "\x1b[F") || strings.HasPrefix(data, "\x1b[4~") {
			ui.moveEnd()
			data = dropEscape(data)
			continue
		}
		if strings.HasPrefix(data, "\x1b[3~") {
			ui.deleteForward()
			data = data[4:]
			continue
		}
		if strings.HasPrefix(data, "\x1b[13;2") || strings.HasPrefix(data, "\x1b[13;5") || strings.HasPrefix(data, "\x1b[13;6") {
			end := strings.IndexAny(data, "u~")
			if end >= 0 {
				ui.insertText("\n")
				data = data[end+1:]
				continue
			}
		}
		if strings.HasPrefix(data, "\x1b") {
			data = dropEscape(data)
			continue
		}
		r, size := utf8.DecodeRuneInString(data)
		if r == utf8.RuneError && size == 0 {
			return
		}
		data = data[size:]
		switch r {
		case '\x01': // Ctrl-A beginning of prompt.
			ui.moveStart()
		case '\x03': // Ctrl-C clears input.
			ui.clearInput()
		case '\x04': // Ctrl-D exits.
			ui.running = false
		case '\x05': // Ctrl-E end of prompt.
			ui.moveEnd()
		case '\x0b': // Ctrl-K kill to end of prompt.
			ui.killEnd()
		case '\x15': // Ctrl-U kill to beginning of prompt.
			ui.killStart()
		case '\x1a': // Ctrl-Z suspends.
			ui.suspend()
		case '\r', '\n':
			ui.submit()
		case '\x7f', '\b':
			ui.backspace()
		default:
			if r >= 32 || r == '\t' {
				ui.insertText(string(r))
			}
		}
	}
}

func normalizePaste(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func dropEscape(s string) string {
	if len(s) <= 1 {
		return ""
	}
	if s[1] == '[' {
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return s[i+1:]
			}
		}
		return ""
	}
	return s[2:]
}

func (ui *TerminalUI) suspend() {
	ui.restore()
	fmt.Print("\033[?2004l\033[?25h\n")
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGTSTP)
	old, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err == nil {
		ui.oldState = old
	}
	fmt.Print("\033[?25l\033[?2004h")
}

func (ui *TerminalUI) insertText(text string) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.clampCursorLocked()
	ui.input = ui.input[:ui.cursor] + text + ui.input[ui.cursor:]
	ui.cursor += len(text)
}

func (ui *TerminalUI) clearInput() {
	ui.mu.Lock()
	ui.input = ""
	ui.cursor = 0
	ui.mu.Unlock()
}

func (ui *TerminalUI) moveStart() {
	ui.mu.Lock()
	ui.cursor = lineStart(ui.input, ui.cursor)
	ui.mu.Unlock()
}

func (ui *TerminalUI) moveEnd() {
	ui.mu.Lock()
	ui.cursor = lineEnd(ui.input, ui.cursor)
	ui.mu.Unlock()
}

func (ui *TerminalUI) moveLeft() {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.clampCursorLocked()
	if ui.cursor == 0 {
		return
	}
	_, size := utf8.DecodeLastRuneInString(ui.input[:ui.cursor])
	ui.cursor -= size
}

func (ui *TerminalUI) moveRight() {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.clampCursorLocked()
	if ui.cursor >= len(ui.input) {
		return
	}
	_, size := utf8.DecodeRuneInString(ui.input[ui.cursor:])
	ui.cursor += size
}

func (ui *TerminalUI) killStart() {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.clampCursorLocked()
	start := lineStart(ui.input, ui.cursor)
	ui.input = ui.input[:start] + ui.input[ui.cursor:]
	ui.cursor = start
}

func (ui *TerminalUI) killEnd() {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.clampCursorLocked()
	end := lineEnd(ui.input, ui.cursor)
	ui.input = ui.input[:ui.cursor] + ui.input[end:]
}

func (ui *TerminalUI) backspace() {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.clampCursorLocked()
	if ui.cursor == 0 {
		return
	}
	_, size := utf8.DecodeLastRuneInString(ui.input[:ui.cursor])
	ui.input = ui.input[:ui.cursor-size] + ui.input[ui.cursor:]
	ui.cursor -= size
}

func (ui *TerminalUI) deleteForward() {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.clampCursorLocked()
	if ui.cursor >= len(ui.input) {
		return
	}
	_, size := utf8.DecodeRuneInString(ui.input[ui.cursor:])
	ui.input = ui.input[:ui.cursor] + ui.input[ui.cursor+size:]
}

func (ui *TerminalUI) submit() {
	ui.mu.Lock()
	text := ui.input
	ui.input = ""
	ui.cursor = 0
	ui.mu.Unlock()
	if ui.onSubmit != nil {
		ui.onSubmit(text)
	}
}

func (ui *TerminalUI) cancel() {
	if ui.onCancel != nil {
		ui.onCancel()
	}
}

func (ui *TerminalUI) Append(role, text string) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.clearLiveLocked()
	ui.commitDeltaLocked()
	ui.items = append(ui.items, transcriptItem{Role: role, Text: text})
	ui.printMessageBlockLocked(role, text, len(ui.items) > 1)
	ui.renderLiveLocked()
}

func (ui *TerminalUI) StartDelta(role string) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.clearLiveLocked()
	if !ui.deltaActive || ui.deltaRole != role {
		ui.commitDeltaLocked()
		ui.items = append(ui.items, transcriptItem{Role: role})
		ui.deltaActive = true
		ui.deltaRole = role
	}
	ui.renderLiveLocked()
}

func (ui *TerminalUI) AppendDelta(role, delta string) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.clearLiveLocked()
	if !ui.deltaActive || ui.deltaRole != role {
		ui.commitDeltaLocked()
		ui.items = append(ui.items, transcriptItem{Role: role})
		ui.deltaActive = true
		ui.deltaRole = role
	}
	ui.items[len(ui.items)-1].Text += delta
	ui.renderLiveLocked()
}

func (ui *TerminalUI) EndDelta() {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.clearLiveLocked()
	ui.commitDeltaLocked()
	ui.renderLiveLocked()
}

func (ui *TerminalUI) DiscardDelta() {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.clearLiveLocked()
	if ui.deltaActive {
		ui.deltaActive = false
		ui.deltaRole = ""
		if len(ui.items) > 0 {
			ui.items = ui.items[:len(ui.items)-1]
		}
	}
	ui.renderLiveLocked()
}

func (ui *TerminalUI) commitDeltaLocked() {
	if !ui.deltaActive {
		return
	}
	idx := len(ui.items) - 1
	item := ui.items[idx]
	ui.deltaActive = false
	ui.deltaRole = ""
	ui.printMessageBlockLocked(item.Role, item.Text, idx > 0)
}

func (ui *TerminalUI) renderLive() {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.clearLiveLocked()
	ui.renderLiveLocked()
}

func (ui *TerminalUI) clearLiveLocked() {
	if ui.liveLines == 0 {
		return
	}
	if ui.cursorRow > 0 {
		fmt.Printf("\033[%dA", ui.cursorRow)
	}
	fmt.Print("\r\033[J")
	ui.liveLines = 0
	ui.cursorRow = 0
}

func (ui *TerminalUI) padToBottomLocked(height int, transcriptRows int) {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		w = 80
	}
	liveRows := len(renderInputLinesOnly(userName(), ui.input, w)) + 1
	if transcriptRows > 0 {
		liveRows++
	}
	if liveRows < 2 {
		liveRows = 2
	}
	pad := bottomPadRows(height, transcriptRows, liveRows)
	if pad > 0 {
		fmt.Print(strings.Repeat("\r\n", pad))
	}
}

func bottomPadRows(height, transcriptRows, liveRows int) int {
	pad := height - transcriptRows - liveRows
	if pad < 0 {
		return 0
	}
	return pad
}

func (ui *TerminalUI) renderLiveLocked() {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		w = 80
	}
	status := ""
	if ui.statusFunc != nil {
		status = ui.statusFunc()
	}
	inputLines, cursorLine, cursorCol := renderInput(userName(), ui.input, ui.cursor, w)
	if len(inputLines) > 12 {
		inputLines = append([]string{dim + fmt.Sprintf("… %d lines above"+reset, len(inputLines)-11)}, inputLines[len(inputLines)-11:]...)
		if cursorLine >= len(inputLines) {
			cursorLine = len(inputLines) - 1
		}
	}

	var activeLines []string
	if ui.deltaActive && len(ui.items) > 0 {
		item := ui.items[len(ui.items)-1]
		activeLines = renderMessage(item.Role, item.Text, w)
	}

	row := 0
	writeLine := func(line string) {
		if row > 0 {
			fmt.Print("\r\n")
		}
		fmt.Print("\033[2K" + line)
		row++
	}
	if len(ui.items) > 0 {
		writeLine("")
	}
	if ui.deltaActive {
		for _, line := range activeLines {
			writeLine(line)
		}
		if len(activeLines) == 0 {
			writeLine("")
			cursorLine = 0
			cursorCol = 0
		} else {
			cursorLine = len(activeLines) - 1
			cursorCol = visibleWidthNoANSI(activeLines[cursorLine])
		}
	} else {
		for _, line := range inputLines {
			writeLine(line)
		}
	}
	writeLine(dim + truncatePlain(status, w) + reset)

	ui.liveLines = row
	ui.cursorRow = cursorLine
	if len(ui.items) > 0 {
		ui.cursorRow++
	}
	if cursorCol >= w {
		cursorCol = w - 1
	}
	up := ui.liveLines - 1 - ui.cursorRow
	if up > 0 {
		fmt.Printf("\033[%dA", up)
	}
	fmt.Printf("\r\033[%dC\033[?25h", cursorCol)
}

func (ui *TerminalUI) printMessageLocked(role, text string) int {
	return ui.printMessageBlockLocked(role, text, false)
}

func (ui *TerminalUI) printMessageBlockLocked(role, text string, leadingBlank bool) int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		w = 80
	}
	rows := 0
	if leadingBlank {
		fmt.Print("\r\n")
		rows++
	}
	lines := renderMessage(role, text, w)
	for i, line := range lines {
		if i > 0 {
			fmt.Print("\r\n")
		}
		fmt.Print(line)
		rows++
	}
	fmt.Print("\r\n")
	return rows
}

func renderMessage(role, text string, width int) []string {
	color := roleColor(role)
	prefix := role + "> "
	if role == "tool" || role == "dur" {
		prefix = role + " "
	}
	return renderPrefixed(color, prefix, text, width)
}

func roleColor(role string) string {
	switch role {
	case userName():
		return red
	case "agent":
		return green
	case "tool":
		return blue
	case "dur":
		return dim
	default:
		return dim
	}
}

func renderInputLinesOnly(name, text string, width int) []string {
	lines, _ := renderPrefixedWithPositions(red, name+"> ", text, width)
	return lines
}

func renderInput(name, text string, cursor int, width int) ([]string, int, int) {
	lines, positions := renderPrefixedWithPositions(red, name+"> ", text, width)
	if len(lines) == 0 {
		return []string{red + name + "> " + reset}, 0, runewidth.StringWidth(name + "> ")
	}
	cursor = clamp(cursor, 0, len(text))
	for i, pos := range positions {
		last := i == len(positions)-1
		if cursor < pos.start || cursor > pos.end || (cursor == pos.end && !last && pos.start != pos.end) {
			continue
		}
		chunkText := stripANSI(lines[i])
		if pos.colBase > 0 && runewidth.StringWidth(chunkText) >= pos.colBase {
			chunkText = trimVisiblePrefix(chunkText, pos.colBase)
		}
		relative := clamp(cursor-pos.start, 0, len(chunkText))
		col := pos.colBase + runewidth.StringWidth(chunkText[:byteIndexForPlainOffset(chunkText, relative)])
		return lines, i, col
	}
	last := len(lines) - 1
	return lines, last, visibleWidthNoANSI(lines[last])
}

func renderPrefixed(color, prefix, text string, width int) []string {
	lines, _ := renderPrefixedWithPositions(color, prefix, text, width)
	return lines
}

type renderPos struct{ start, end, colBase int }

func renderPrefixedWithPositions(color, prefix, text string, width int) ([]string, []renderPos) {
	if width < 10 {
		width = 10
	}
	prefixWidth := runewidth.StringWidth(prefix)
	bodyWidth := width - prefixWidth
	if bodyWidth < 8 {
		bodyWidth = width
		prefixWidth = 0
		prefix = ""
	}
	if text == "" {
		return []string{color + prefix + reset}, []renderPos{{start: 0, end: 0, colBase: prefixWidth}}
	}
	physical := strings.Split(text, "\n")
	var out []string
	var positions []renderPos
	offset := 0
	for i, line := range physical {
		chunks := wrapPlainWithOffsets(line, bodyWidth)
		for j, chunk := range chunks {
			p := strings.Repeat(" ", prefixWidth)
			if i == 0 && j == 0 {
				p = color + prefix + reset
			}
			out = append(out, p+chunk.text)
			positions = append(positions, renderPos{start: offset + chunk.start, end: offset + chunk.end, colBase: visibleWidthNoANSI(p)})
		}
		offset += len(line)
		if i < len(physical)-1 {
			offset++
		}
	}
	return out, positions
}

type textChunk struct {
	text       string
	start, end int
}

func wrapPlainWithOffsets(s string, width int) []textChunk {
	if s == "" {
		return []textChunk{{text: "", start: 0, end: 0}}
	}
	var out []textChunk
	var b strings.Builder
	col := 0
	chunkStart := 0
	for idx, r := range s {
		rw := runewidth.RuneWidth(r)
		if col+rw > width && col > 0 {
			out = append(out, textChunk{text: b.String(), start: chunkStart, end: idx})
			b.Reset()
			col = 0
			chunkStart = idx
		}
		b.WriteRune(r)
		col += rw
	}
	out = append(out, textChunk{text: b.String(), start: chunkStart, end: len(s)})
	return out
}

func (ui *TerminalUI) clampCursorLocked() {
	ui.cursor = clamp(ui.cursor, 0, len(ui.input))
	for ui.cursor > 0 && ui.cursor < len(ui.input) && !utf8.RuneStart(ui.input[ui.cursor]) {
		ui.cursor--
	}
}

func lineStart(s string, cursor int) int {
	cursor = clamp(cursor, 0, len(s))
	if idx := strings.LastIndex(s[:cursor], "\n"); idx >= 0 {
		return idx + 1
	}
	return 0
}

func lineEnd(s string, cursor int) int {
	cursor = clamp(cursor, 0, len(s))
	if idx := strings.Index(s[cursor:], "\n"); idx >= 0 {
		return cursor + idx
	}
	return len(s)
}

func clamp(n, min, max int) int {
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func byteIndexForPlainOffset(s string, offset int) int {
	if offset <= 0 {
		return 0
	}
	if offset >= len(s) {
		return len(s)
	}
	for i := range s {
		if i >= offset {
			return i
		}
	}
	return len(s)
}

func trimVisiblePrefix(s string, width int) string {
	col := 0
	for i, r := range s {
		if col >= width {
			return s[i:]
		}
		col += runewidth.RuneWidth(r)
	}
	return ""
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			i = len(s) - len(dropEscape(s[i:]))
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

func visibleWidthNoANSI(s string) int {
	width := 0
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			i = len(s) - len(dropEscape(s[i:]))
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		width += runewidth.RuneWidth(r)
		i += size
	}
	return width
}

func truncatePlain(s string, width int) string {
	if runewidth.StringWidth(s) <= width {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if col+rw > width-1 {
			break
		}
		b.WriteRune(r)
		col += rw
	}
	return b.String() + "…"
}

func (s *Session) beginTurn() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	s.turnMu.Lock()
	if s.turnCancel != nil {
		s.turnCancel()
	}
	s.turnID++
	id := s.turnID
	s.turnCancel = cancel
	s.turnMu.Unlock()
	finish := func() {
		cancel()
		s.turnMu.Lock()
		if s.turnID == id {
			s.turnCancel = nil
		}
		s.turnMu.Unlock()
	}
	return ctx, finish
}

func (s *Session) cancelTurn() bool {
	s.turnMu.Lock()
	cancel := s.turnCancel
	s.turnMu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (s *Session) command(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	s.UI.Append(userName(), line)
	switch fields[0] {
	case "/exit", "/quit":
		return true
	case "/help":
		s.UI.Append("dur", helpText)
	case "/pwd":
		s.UI.Append("dur", s.Runner.Cwd)
	case "/paste":
		s.UI.Append("dur", "Paste normally. Bracketed paste is enabled, so pasted newlines are inserted into the draft instead of submitting turns.")
	case "/cd":
		if len(fields) != 2 {
			s.UI.Append("dur", "Usage: /cd <path>")
			break
		}
		p := fields[1]
		if strings.HasPrefix(p, "~/") {
			if h, err := os.UserHomeDir(); err == nil {
				p = filepath.Join(h, p[2:])
			}
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(s.Runner.Cwd, p)
		}
		p = filepath.Clean(p)
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			s.UI.Append("dur", "not a directory: "+fields[1])
			break
		}
		s.Runner.Cwd = p
		s.UI.Append("dur", p)
	case "/tools":
		if len(fields) == 3 && fields[1] == "verbose" && (fields[2] == "on" || fields[2] == "off") {
			s.Runner.Verbose = fields[2] == "on"
			s.UI.Append("dur", "tool verbosity: "+fields[2])
			break
		}
		if len(fields) == 2 && fields[1] == "history" {
			s.printToolHistory()
			break
		}
		s.UI.Append("dur", toolsText)
	case "/tool":
		s.showTool(fields)
	case "/models":
		s.printModels()
	case "/model":
		if len(fields) != 2 {
			s.UI.Append("dur", "Usage: /model <model>")
			break
		}
		if !provider.ResponseModelID(fields[1]) {
			s.UI.Append("dur", "unsupported Responses model: "+fields[1])
			break
		}
		s.Cfg.Model = fields[1]
		if err := config.Save(s.Cfg); err != nil {
			s.UI.Append("dur", err.Error())
			break
		}
		s.Model, _ = config.EffectiveModel(s.Cfg)
		s.UI.Append("dur", "model set to "+fields[1])
	case "/thinking":
		if len(fields) != 2 || !config.ValidThinking(fields[1]) {
			s.UI.Append("dur", "Usage: /thinking minimal|low|medium|high")
			break
		}
		s.Cfg.Thinking = fields[1]
		if err := config.Save(s.Cfg); err != nil {
			s.UI.Append("dur", err.Error())
			break
		}
		s.Thinking, _ = config.EffectiveThinking(s.Cfg)
		s.UI.Append("dur", "thinking set to "+fields[1])
	case "/config", "/status":
		cfg := config.Load()
		model, src := config.EffectiveModel(cfg)
		thinking, thinkingSrc := config.EffectiveThinking(cfg)
		s.Model = model
		s.Thinking = thinking
		s.UI.Append("dur", fmt.Sprintf("model: %s (%s)\nthinking: %s (%s)\ndebug: %s\ntool cwd: %s\ntool verbosity: %s\ntool calls: %d\nconfig: %s", model, src, thinking, thinkingSrc, onoff(s.Debug), s.Runner.Cwd, onoff(s.Runner.Verbose), len(s.Runner.Records), config.Path()))
	case "/debug":
		if len(fields) != 2 || !in(fields[1], "on", "off") {
			s.UI.Append("dur", "Usage: /debug on|off")
			break
		}
		s.Debug = fields[1] == "on"
		s.UI.Append("dur", "debug: "+fields[1])
	default:
		s.UI.Append("dur", "unknown chat command: "+fields[0])
	}
	return false
}

func (s *Session) turn() error {
	ctx, finish := s.beginTurn()
	defer finish()
	toolCalls := 0
	for round := 0; round < 12; round++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		req := provider.Request{Model: s.Model, Instructions: provider.ChatPrompt, Input: s.History, Reasoning: provider.Reasoning(s.Thinking), Tools: []provider.ToolSchema{provider.ToolDefinition()}}
		if s.Debug {
			s.UI.Append("dur", debugRequestString(req))
		}
		var agent strings.Builder
		s.UI.StartDelta("agent")
		res, err := s.Provider.Stream(ctx, req, func(delta string) {
			agent.WriteString(delta)
			s.UI.AppendDelta("agent", delta)
		})
		if err != nil {
			if agent.Len() == 0 {
				s.UI.DiscardDelta()
			} else {
				s.UI.EndDelta()
			}
			return err
		}
		if len(res.ToolCalls) == 0 {
			if res.Answer == "" {
				res.Answer = agent.String()
			} else if agent.Len() == 0 {
				s.UI.AppendDelta("agent", res.Answer)
			}
			if res.Answer == "" {
				s.UI.DiscardDelta()
			} else {
				s.UI.EndDelta()
				s.History = append(s.History, map[string]any{"role": "assistant", "content": res.Answer})
			}
			return nil
		}
		if agent.Len() == 0 {
			s.UI.DiscardDelta()
		} else {
			s.UI.EndDelta()
		}
		for _, call := range res.ToolCalls {
			if err := ctx.Err(); err != nil {
				return err
			}
			var args struct {
				Cmd  string   `json:"cmd"`
				Args []string `json:"args"`
			}
			_ = json.Unmarshal([]byte(call.Arguments), &args)
			rec := s.Runner.Run(args.Cmd, args.Args)
			s.UI.Append("tool", plainToolLine(rec, false))
			if s.Runner.Verbose {
				s.UI.Append("tool", plainToolLine(rec, true)+"\n"+renderToolResultPlain(rec.Result))
			}
			s.History = append(s.History, map[string]any{"type": "function_call", "call_id": call.CallID, "name": call.Name, "arguments": call.Arguments})
			s.History = append(s.History, map[string]any{"type": "function_call_output", "call_id": call.CallID, "output": rec.Result})
			toolCalls++
		}
		if toolCalls >= 40 {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.History = append(s.History, map[string]any{"role": "user", "content": "Tool-call limit reached. Do not request more tools. Summarize findings from tool results already provided."})
	var agent strings.Builder
	s.UI.StartDelta("agent")
	res, err := s.Provider.Stream(ctx, provider.Request{Model: s.Model, Instructions: provider.ChatPrompt, Input: s.History, Reasoning: provider.Reasoning(s.Thinking)}, func(delta string) {
		agent.WriteString(delta)
		s.UI.AppendDelta("agent", delta)
	})
	if err != nil {
		if agent.Len() == 0 {
			s.UI.DiscardDelta()
		} else {
			s.UI.EndDelta()
		}
		return err
	}
	if res.Answer == "" {
		res.Answer = agent.String()
	} else if agent.Len() == 0 {
		s.UI.AppendDelta("agent", res.Answer)
	}
	if res.Answer == "" {
		s.UI.DiscardDelta()
	} else {
		s.UI.EndDelta()
		s.History = append(s.History, map[string]any{"role": "assistant", "content": res.Answer})
	}
	return nil
}

func (s *Session) printModels() {
	models, _ := s.Provider.Models(context.Background())
	var b strings.Builder
	for _, m := range models {
		mark := " "
		if m == s.Model {
			mark = "*"
		}
		fmt.Fprintln(&b, mark, m)
	}
	s.UI.Append("dur", strings.TrimRight(b.String(), "\n"))
}

func (s *Session) printToolHistory() {
	if len(s.Runner.Records) == 0 {
		s.UI.Append("dur", "no tool calls yet")
		return
	}
	var b strings.Builder
	for _, rec := range s.Runner.Records {
		fmt.Fprintln(&b, plainToolLine(rec, false))
	}
	s.UI.Append("tool", strings.TrimRight(b.String(), "\n"))
}

func (s *Session) showTool(fields []string) {
	if len(fields) != 2 {
		s.UI.Append("dur", "Usage: /tool N")
		return
	}
	var rec tools.Record
	var ok bool
	if fields[1] == "last" {
		rec, ok = s.Runner.Last()
	} else {
		id, err := strconv.Atoi(fields[1])
		if err != nil {
			s.UI.Append("dur", "Usage: /tool N")
			return
		}
		rec, ok = s.Runner.Get(id)
	}
	if !ok {
		s.UI.Append("dur", "no such tool call")
		return
	}
	s.UI.Append("tool", plainToolLine(rec, true)+"\n"+renderToolResultPlain(rec.Result))
}

func plainToolLine(rec tools.Record, expanded bool) string {
	glyph := "▸"
	status := "✓"
	if expanded {
		glyph = "▾"
	}
	if rec.Denied {
		status = "✕"
	}
	return fmt.Sprintf("tool %d %s %s %s %s", rec.ID, status, glyph, rec.Trace, rec.Elapsed.Round(10_000_000))
}

func renderToolResultPlain(result string) string {
	parts := parseToolResult(result)
	if len(parts) == 0 {
		return result
	}
	var b strings.Builder
	if code := parts["exit_code"]; code != "" {
		b.WriteString("exit ")
		b.WriteString(code)
		b.WriteString("\n")
	}
	if stdout := parts["stdout"]; stdout != "" {
		b.WriteString("\nstdout\n──────\n")
		b.WriteString(strings.TrimRight(stdout, "\n"))
		b.WriteString("\n")
	}
	if stderr := parts["stderr"]; stderr != "" {
		b.WriteString("\nstderr\n──────\n")
		b.WriteString(strings.TrimRight(stderr, "\n"))
		b.WriteString("\n")
	}
	out := strings.TrimRight(b.String(), "\n")
	if out == "" {
		return result
	}
	return out
}

func parseToolResult(result string) map[string]string {
	sections := map[string]string{}
	lines := strings.Split(result, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "exit_code: ") {
			sections["exit_code"] = strings.TrimSpace(strings.TrimPrefix(line, "exit_code: "))
			continue
		}
		if line == "stdout:" || line == "stderr:" {
			key := strings.TrimSuffix(line, ":")
			start := i + 1
			j := start
			for j < len(lines) && lines[j] != "stdout:" && lines[j] != "stderr:" {
				j++
			}
			sections[key] = strings.Join(lines[start:j], "\n")
			i = j - 1
		}
	}
	return sections
}

func (s *Session) statusLine() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "host"
	}
	if dot := strings.IndexByte(host, '.'); dot > 0 {
		host = host[:dot]
	}
	cwd := shortPath(s.Runner.Cwd)
	return fmt.Sprintf("%s:%s | %s | thinking:%s | tools:on | verbose:%s", host, cwd, s.Model, s.Thinking, onoff(s.Runner.Verbose))
}

func userName() string {
	name := os.Getenv("USER")
	if name == "" {
		name = os.Getenv("LOGNAME")
	}
	if name == "" {
		name = "you"
	}
	return name
}

func shortPath(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+string(os.PathSeparator)) {
			return "~" + strings.TrimPrefix(path, home)
		}
	}
	return path
}

func debugRequestString(req provider.Request) string {
	b, _ := json.MarshalIndent(req, "", "  ")
	return "--- dur request ---\n" + string(b) + "\n--- end dur request ---"
}

func onoff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func in(s string, xs ...string) bool {
	for _, x := range xs {
		if s == x {
			return true
		}
	}
	return false
}

const helpText = `dur chat commands:
  /help
  /config
  /status
  /models
  /model <id>
  /thinking minimal|low|medium|high
  /debug on|off
  /paste
  /tools
  /tools history
  /tools verbose on|off
  /tool N
  /tool last
  /pwd
  /cd <path>
  /exit
  /quit

input:
  Enter sends message
  Bracketed paste inserts multiline content normally
  Shift-Enter inserts newline if your terminal supports it
  Ctrl-C clears current prompt
  Ctrl-D exits
  Ctrl-Z suspends`

const toolsText = `read-only tools:
  pwd ls stat file wc head tail cat rg grep
  df free uptime uname id whoami hostname ps ss ip
  dig whois ping dmesg journalctl systemctl docker find`
