package chat

import (
	"context"
	"strings"
	"sync"

	"github.com/adamshand/aidur/internal/config"
	"github.com/adamshand/aidur/internal/provider"
	"github.com/adamshand/aidur/internal/tools"
	"golang.org/x/term"
)

type Session struct {
	Provider     provider.Client
	Cfg          config.Config
	Model        string
	ModelSource  string
	Thinking     string
	Instructions string
	Debug        bool
	Runner       *tools.Runner
	History      []map[string]any
	UI           *TerminalUI
	turnMu       sync.Mutex
	turnID       uint64
	turnCancel   context.CancelFunc
}

type transcriptItem struct {
	Role         string
	Text         string
	Tool         *tools.Record
	ToolExpanded bool
	PrintedRows  int
}

type TerminalUI struct {
	mu             sync.Mutex
	items          []transcriptItem
	input          string
	cursor         int
	killRing       string
	inputHistory   []string
	historyIndex   int
	historyDraft   string
	oldState       *term.State
	pasteMode      bool
	pasteBuf       strings.Builder
	liveLines      int
	cursorRow      int
	renderer       liveRenderer
	running        bool
	statusFunc     func() string
	onSubmit       func(string)
	onCancel       func()
	agentPrompt    string
	deltaActive    bool
	deltaRole      string
	deltaSpinID    uint64
	deltaSpinFrame int
}
