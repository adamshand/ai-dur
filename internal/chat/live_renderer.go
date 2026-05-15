package chat

import "fmt"

const (
	beginSynchronizedOutput = "\x1b[?2026h"
	endSynchronizedOutput   = "\x1b[?2026l"
)

type liveFrame struct {
	Lines     []string
	CursorRow int
	CursorCol int
}

type liveRenderer struct {
	liveLines int
	cursorRow int
	lastFrame liveFrame
}

func (r *liveRenderer) Clear() {
	if r.liveLines == 0 {
		return
	}
	fmt.Print(beginSynchronizedOutput)
	if r.cursorRow > 0 {
		fmt.Printf("\033[%dA", r.cursorRow)
	}
	fmt.Print("\r\033[J")
	r.liveLines = 0
	r.cursorRow = 0
	r.lastFrame = liveFrame{}
	fmt.Print(endSynchronizedOutput)
}

func (r *liveRenderer) Render(frame liveFrame) {
	if liveFramesEqual(frame, r.lastFrame) {
		return
	}
	fmt.Print(beginSynchronizedOutput)
	if r.liveLines > 0 {
		if r.cursorRow > 0 {
			fmt.Printf("\033[%dA", r.cursorRow)
		}
		fmt.Print("\r\033[J")
	}
	for i, line := range frame.Lines {
		if i > 0 {
			fmt.Print("\r\n")
		}
		fmt.Print("\033[2K" + line)
	}
	r.liveLines = len(frame.Lines)
	r.cursorRow = frame.CursorRow
	r.lastFrame = frame
	cursorCol := frame.CursorCol
	if cursorCol < 0 {
		cursorCol = 0
	}
	up := r.liveLines - 1 - r.cursorRow
	if up > 0 {
		fmt.Printf("\033[%dA", up)
	}
	fmt.Printf("\r\033[%dC\033[?25h", cursorCol)
	fmt.Print(endSynchronizedOutput)
}

func liveFramesEqual(a, b liveFrame) bool {
	if a.CursorRow != b.CursorRow || a.CursorCol != b.CursorCol || len(a.Lines) != len(b.Lines) {
		return false
	}
	for i := range a.Lines {
		if a.Lines[i] != b.Lines[i] {
			return false
		}
	}
	return true
}
