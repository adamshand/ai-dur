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
	Thinking string
	Debug    bool
	Runner   *tools.Runner
	History  []map[string]any
}

func Run(debug bool) int {
	cfg := config.Load()
	model, _ := config.EffectiveModel(cfg)
	thinking, _ := config.EffectiveThinking(cfg)
	cwd, _ := os.Getwd()
	s := &Session{Provider: provider.New(), Cfg: cfg, Model: model, Thinking: thinking, Debug: debug, Runner: tools.NewRunner(cwd)}
	rl, err := readline.NewEx(&readline.Config{Prompt: s.inputPrompt(), HistoryLimit: 200})
	if err != nil {
		fmt.Fprintln(os.Stderr, "dur:", err)
		return 1
	}
	defer rl.Close()
	fmt.Println("dur chat: ephemeral session; read-only tools enabled")
	fmt.Println("type /help for commands, /exit to quit")
	for {
		fmt.Println(s.statusLine())
		rl.SetPrompt(s.inputPrompt())
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
			if s.command(line, rl) {
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

func (s *Session) command(line string, rl *readline.Instance) bool {
	fields := strings.Fields(line)
	switch fields[0] {
	case "/exit", "/quit":
		return true
	case "/help":
		fmt.Println(helpText)
	case "/pwd":
		fmt.Println(s.Runner.Cwd)
	case "/paste":
		s.paste(rl)
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
		if len(fields) == 2 && fields[1] == "history" {
			s.printToolHistory()
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
		printToolExpanded(rec)
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
	case "/thinking":
		if len(fields) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: /thinking minimal|low|medium|high")
			break
		}
		if !config.ValidThinking(fields[1]) {
			fmt.Fprintln(os.Stderr, "dur: unsupported thinking effort:", fields[1])
			break
		}
		s.Cfg.Thinking = fields[1]
		if err := config.Save(s.Cfg); err != nil {
			fmt.Fprintln(os.Stderr, "dur:", err)
			break
		}
		s.Thinking, _ = config.EffectiveThinking(s.Cfg)
		fmt.Println("dur: thinking set to", fields[1])
	case "/config", "/status":
		cfg := config.Load()
		model, src := config.EffectiveModel(cfg)
		thinking, thinkingSrc := config.EffectiveThinking(cfg)
		s.Model = model
		s.Thinking = thinking
		fmt.Println("model:", model, "("+src+")")
		fmt.Println("thinking:", thinking, "("+thinkingSrc+")")
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
		req := provider.Request{Model: s.Model, Instructions: provider.ChatPrompt, Input: s.History, Reasoning: provider.Reasoning(s.Thinking), Tools: []provider.ToolSchema{provider.ToolDefinition()}}
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
			printToolCollapsed(rec)
			if s.Runner.Verbose {
				printToolExpanded(rec)
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
	res, err := s.Provider.Stream(context.Background(), provider.Request{Model: s.Model, Instructions: provider.ChatPrompt, Input: s.History, Reasoning: provider.Reasoning(s.Thinking)}, func(delta string) {
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

func (s *Session) paste(rl *readline.Instance) {
	fmt.Println("paste mode: paste text, then enter a line containing only .")
	oldPrompt := s.inputPrompt()
	defer rl.SetPrompt(oldPrompt)
	var b strings.Builder
	for {
		rl.SetPrompt("\033[2mpaste>\033[0m ")
		line, err := rl.Readline()
		if err == readline.ErrInterrupt {
			fmt.Println("paste cancelled")
			return
		}
		if err != nil {
			fmt.Println()
			return
		}
		if strings.TrimSpace(line) == "." {
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	text := strings.TrimRight(b.String(), "\n")
	if strings.TrimSpace(text) == "" {
		fmt.Println("paste cancelled: no content")
		return
	}
	s.History = append(s.History, map[string]any{"role": "user", "content": text})
	if err := s.turn(); err != nil {
		fmt.Fprintln(os.Stderr, "dur:", err)
	}
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

func (s *Session) printToolHistory() {
	if len(s.Runner.Records) == 0 {
		fmt.Println("no tool calls yet")
		return
	}
	for _, rec := range s.Runner.Records {
		fmt.Println(toolLine(rec, false))
	}
}

func printToolCollapsed(rec tools.Record) {
	fmt.Println(toolLine(rec, false))
}

func printToolExpanded(rec tools.Record) {
	fmt.Println()
	fmt.Println(toolLine(rec, true))
	fmt.Println(renderToolResult(rec.Result))
	fmt.Println()
}

func toolLine(rec tools.Record, expanded bool) string {
	glyph := "▸"
	status := "✓"
	if expanded {
		glyph = "▾"
	}
	if rec.Denied {
		status = "✕"
	}
	return fmt.Sprintf("\033[34mtool %d %s %s %s\033[2m %s\033[0m", rec.ID, status, glyph, rec.Trace, rec.Elapsed.Round(10_000_000))
}

func renderToolResult(result string) string {
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
		b.WriteString("\n\033[2mstdout\n──────\033[0m\n")
		b.WriteString(strings.TrimRight(stdout, "\n"))
		b.WriteString("\n")
	}
	if stderr := parts["stderr"]; stderr != "" {
		b.WriteString("\n\033[2mstderr\n──────\033[0m\n")
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
	return fmt.Sprintf("\033[2m%s:%s | %s | thinking:%s | tools:on | verbose:%s\033[0m", host, cwd, s.Model, s.Thinking, onoff(s.Runner.Verbose))
}

func (s *Session) inputPrompt() string {
	name := os.Getenv("USER")
	if name == "" {
		name = os.Getenv("LOGNAME")
	}
	if name == "" {
		name = "you"
	}
	return fmt.Sprintf("\033[31m%s>\033[0m ", name)
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
  /quit`

const toolsText = `read-only tools:
  pwd ls stat file wc head tail cat rg grep
  df free uptime uname id whoami hostname ps ss ip
  dig whois ping dmesg journalctl systemctl docker find`
