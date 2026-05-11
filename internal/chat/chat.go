package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/adamshand/aidur/internal/config"
	"github.com/adamshand/aidur/internal/provider"
	"github.com/adamshand/aidur/internal/tools"
	"github.com/chzyer/readline"
)

type Session struct {
	Provider provider.Client
	Cfg      config.Config
	Model    string
	Debug    bool
	Runner   *tools.Runner
	History  []map[string]any
}

func Run(debug bool) int {
	cfg := config.Load()
	model, _ := config.EffectiveModel(cfg)
	cwd, _ := os.Getwd()
	s := &Session{Provider: provider.New(), Cfg: cfg, Model: model, Debug: debug, Runner: tools.NewRunner(cwd)}
	rl, err := readline.NewEx(&readline.Config{Prompt: "\033[31mdur>\033[0m ", HistoryLimit: 200})
	if err != nil {
		fmt.Fprintln(os.Stderr, "dur:", err)
		return 1
	}
	defer rl.Close()
	fmt.Println("dur chat: ephemeral session; read-only tools enabled")
	fmt.Println("type /help for commands, /exit to quit")
	for {
		line, err := rl.Readline()
		if err == readline.ErrInterrupt {
			fmt.Println()
			return 130
		}
		if err != nil {
			fmt.Println()
			return 0
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			if s.command(line) {
				return 0
			}
			continue
		}
		s.History = append(s.History, map[string]any{"role": "user", "content": line})
		if err := s.turn(); err != nil {
			fmt.Fprintln(os.Stderr, "dur:", err)
		}
	}
}

func (s *Session) command(line string) bool {
	fields := strings.Fields(line)
	switch fields[0] {
	case "/exit", "/quit":
		return true
	case "/help":
		fmt.Println(helpText)
	case "/pwd":
		fmt.Println(s.Runner.Cwd)
	case "/cd":
		if len(fields) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: /cd <path>")
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
			fmt.Fprintln(os.Stderr, "dur: not a directory:", fields[1])
			break
		}
		s.Runner.Cwd = p
		fmt.Println(p)
	case "/tools":
		if len(fields) == 3 && fields[1] == "verbose" && (fields[2] == "on" || fields[2] == "off") {
			s.Runner.Verbose = fields[2] == "on"
			fmt.Println("tool verbosity:", fields[2])
			break
		}
		fmt.Println(toolsText)
	case "/tool":
		if len(fields) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: /tool N")
			break
		}
		var rec tools.Record
		var ok bool
		if fields[1] == "last" {
			rec, ok = s.Runner.Last()
		} else {
			id, err := strconv.Atoi(fields[1])
			if err != nil {
				fmt.Fprintln(os.Stderr, "Usage: /tool N")
				break
			}
			rec, ok = s.Runner.Get(id)
		}
		if !ok {
			fmt.Fprintln(os.Stderr, "dur: no such tool call")
			break
		}
		fmt.Printf("\n\033[34mtool %d>\033[0m %s\n%s\n\n", rec.ID, rec.Trace, rec.Result)
	case "/models":
		s.printModels()
	case "/model":
		if len(fields) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: /model <model>")
			break
		}
		if !provider.ResponseModelID(fields[1]) {
			fmt.Fprintln(os.Stderr, "dur: unsupported Responses model:", fields[1])
			break
		}
		s.Cfg.Model = fields[1]
		if err := config.Save(s.Cfg); err != nil {
			fmt.Fprintln(os.Stderr, "dur:", err)
			break
		}
		s.Model, _ = config.EffectiveModel(s.Cfg)
		fmt.Println("dur: model set to", fields[1])
	case "/config", "/status":
		model, src := config.EffectiveModel(config.Load())
		s.Model = model
		fmt.Println("model:", model, "("+src+")")
		fmt.Println("debug:", onoff(s.Debug))
		fmt.Println("tool cwd:", s.Runner.Cwd)
		fmt.Println("tool verbosity:", onoff(s.Runner.Verbose))
		fmt.Println("tool calls:", len(s.Runner.Records))
		fmt.Println("config:", config.Path())
	case "/debug":
		if len(fields) != 2 || !in(fields[1], "on", "off") {
			fmt.Fprintln(os.Stderr, "Usage: /debug on|off")
			break
		}
		s.Debug = fields[1] == "on"
		fmt.Println("debug:", fields[1])
	default:
		fmt.Fprintln(os.Stderr, "dur: unknown chat command:", fields[0])
	}
	return false
}

func (s *Session) turn() error {
	toolCalls := 0
	for round := 0; round < 12; round++ {
		req := provider.Request{Model: s.Model, Instructions: provider.ChatPrompt, Input: s.History, Tools: []provider.ToolSchema{provider.ToolDefinition()}}
		if s.Debug {
			debugRequest(req)
		}
		printedAgent := false
		res, err := s.Provider.Stream(context.Background(), req, func(delta string) {
			if !printedAgent {
				fmt.Print("\n\033[32magent>\033[0m ")
				printedAgent = true
			}
			fmt.Print(delta)
		})
		if err != nil {
			return err
		}
		if len(res.ToolCalls) == 0 {
			if printedAgent {
				fmt.Print("\n\n")
			} else {
				fmt.Printf("\n\033[32magent>\033[0m %s\n\n", res.Answer)
			}
			s.History = append(s.History, map[string]any{"role": "assistant", "content": res.Answer})
			return nil
		}
		if printedAgent {
			fmt.Print("\n")
		}
		for _, call := range res.ToolCalls {
			var args struct {
				Cmd  string   `json:"cmd"`
				Args []string `json:"args"`
			}
			_ = json.Unmarshal([]byte(call.Arguments), &args)
			rec := s.Runner.Run(args.Cmd, args.Args)
			label := fmt.Sprintf("[tool %d]", rec.ID)
			if rec.Denied {
				label = fmt.Sprintf("[tool %d denied]", rec.ID)
			}
			fmt.Fprintf(os.Stderr, "\033[34m%s %s\033[0m\n", label, rec.Trace)
			if s.Runner.Verbose {
				fmt.Printf("\n\033[34mtool %d>\033[0m %s\n%s\n\n", rec.ID, rec.Trace, rec.Result)
			}
			s.History = append(s.History, map[string]any{"type": "function_call", "call_id": call.CallID, "name": call.Name, "arguments": call.Arguments})
			s.History = append(s.History, map[string]any{"type": "function_call_output", "call_id": call.CallID, "output": rec.Result})
			toolCalls++
		}
		if toolCalls >= 40 {
			break
		}
	}
	s.History = append(s.History, map[string]any{"role": "user", "content": "Tool-call limit reached. Do not request more tools. Summarize findings from tool results already provided."})
	printedAgent := false
	res, err := s.Provider.Stream(context.Background(), provider.Request{Model: s.Model, Instructions: provider.ChatPrompt, Input: s.History}, func(delta string) {
		if !printedAgent {
			fmt.Print("\n\033[32magent>\033[0m ")
			printedAgent = true
		}
		fmt.Print(delta)
	})
	if err != nil {
		return err
	}
	if printedAgent {
		fmt.Print("\n\n")
	} else {
		fmt.Printf("\n\033[32magent>\033[0m %s\n\n", res.Answer)
	}
	return nil
}

func (s *Session) printModels() {
	models, _ := s.Provider.Models(context.Background())
	for _, m := range models {
		mark := " "
		if m == s.Model {
			mark = "*"
		}
		fmt.Println(mark, m)
	}
}

func debugRequest(req provider.Request) {
	b, _ := json.MarshalIndent(req, "", "  ")
	fmt.Fprintln(os.Stderr, "--- dur request ---")
	fmt.Fprintln(os.Stderr, string(b))
	fmt.Fprintln(os.Stderr, "--- end dur request ---")
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
  /debug on|off
  /tools
  /tools verbose on|off
  /tool N
  /tool last
  /pwd
  /cd <path>
  /exit
  /quit`

const toolsText = `read-only tools:
  pwd ls stat file wc head tail cat rg grep
  df free uptime uname id whoami hostname ps ss ip
  dig whois ping dmesg journalctl systemctl docker find`
