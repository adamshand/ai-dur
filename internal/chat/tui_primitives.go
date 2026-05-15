package chat

import "strings"

// Component is the small rendering primitive used by the chat TUI. Rendered
// lines must fit within the provided width.
type Component interface {
	Render(width int) []string
	Invalidate()
}

// Focusable components can consume normalized keyboard input.
type Focusable interface {
	Component
	HandleKey(KeyEvent) bool
}

type Key string

const (
	KeyRune      Key = "rune"
	KeyEnter     Key = "enter"
	KeyEscape    Key = "escape"
	KeyTab       Key = "tab"
	KeyBackspace Key = "backspace"
	KeyDelete    Key = "delete"
	KeyUp        Key = "up"
	KeyDown      Key = "down"
	KeyLeft      Key = "left"
	KeyRight     Key = "right"
	KeyHome      Key = "home"
	KeyEnd       Key = "end"
	KeyUnknown   Key = "unknown"
)

type KeyModifiers struct {
	Shift bool
	Alt   bool
	Ctrl  bool
	Super bool
}

type KeyEvent struct {
	Key  Key
	Rune rune
	Mods KeyModifiers
}

func (e KeyEvent) HasModifier() bool {
	return e.Mods.Shift || e.Mods.Alt || e.Mods.Ctrl || e.Mods.Super
}

// sanitizeDisplayText removes terminal control sequences from untrusted text
// before rendering. Newlines and tabs are preserved; styling is applied by dur
// after sanitization.
func sanitizeDisplayText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c == '\x1b' {
			i = skipEscapeSequence(s, i)
			continue
		}
		if c < 0x20 || c == 0x7f {
			if c == '\n' || c == '\t' {
				b.WriteByte(c)
			}
			i++
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

func skipEscapeSequence(s string, i int) int {
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[':
		for j := i + 2; j < len(s); j++ {
			if s[j] >= 0x40 && s[j] <= 0x7e {
				return j + 1
			}
		}
		return len(s)
	case ']':
		return skipUntilStringTerminator(s, i+2)
	case 'P', '^', '_':
		return skipUntilStringTerminator(s, i+2)
	default:
		return i + 2
	}
}

func skipUntilStringTerminator(s string, i int) int {
	for i < len(s) {
		if s[i] == '\a' {
			return i + 1
		}
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\' {
			return i + 2
		}
		i++
	}
	return len(s)
}
