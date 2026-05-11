# Go rewrite plan for `dur`

## Module and binary

- Module: `github.com/adamshand/aidur`
- Binary: `dur`
- Build target: `go build -o dur ./cmd/dur`

## CLI shape

```sh
dur                         # ephemeral chat
dur [--debug] <question>     # one-shot question
command | dur [question]     # one-shot with stdin context
dur --help|-h|help           # usage
dur --version                # version
```

No top-level config/model/status subcommands in the Go UX. Those belong inside chat.

## Chat commands

```text
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
/quit
```

Chat is ephemeral. Conversation history, readline history, and tool results are memory-only.

## Clipboard

No OSC 52 clipboard feature in Go v1. Users can paste into chat, pipe stdin into one-shot mode, or ask tools to inspect local files/logs/services.

## Streaming

Streaming is on by default with no normal user-facing streaming toggle. The provider layer parses SSE text deltas and buffers function-call argument deltas until a complete tool call is available. Tools are never executed from partial streamed arguments.

## Provider

V1 targets OpenAI-compatible Responses API:

```text
POST $OPENCODE_BASE_URL/v1/responses
Authorization: Bearer $OPENCODE_ZEN_API_KEY
```

Defaults:

```text
OPENCODE_BASE_URL = https://opencode.ai/zen
AIDUR_MODEL       = saved config model or gpt-5.4-mini
```

Keep provider code isolated so later providers can be added: OpenRouter, OpenAI/Codex OAuth, or opencode SDK-based APIs.

## Tools

Expose one model tool: `run_readonly_command {cmd,args}`.

Rules:

- no shell
- trusted executable path only
- no stdin
- timeout
- output cap
- secret redaction
- denied tools return normal tool output with `exit_code: 2` and `stderr: denied: ...`
- compact trace by default: `[tool N] cmd args` or `[tool N denied] cmd args`
- `/tool N` expands command/stdout/stderr

Allowed commands:

```text
pwd ls stat file wc head tail cat rg grep
df free uptime uname id whoami hostname ps ss ip
dig whois ping dmesg journalctl systemctl docker find
```

Important validators copied from Python prototype:

- deny shell syntax tokens
- deny shells/interpreters by allowlist absence
- `rg`: force `--no-config`, deny `--pre`, `--pre-glob`, `--hidden`, `--no-ignore`, `-u*`
- `grep`: deny recursive modes; use `rg` instead
- `tail`: deny follow
- `journalctl`: deny follow/vacuum/rotate/flush/state-writing options, force `--no-pager`, default `-n 200`, cap supplied lines
- `systemctl`: read-only subcommands only
- `docker`: only `ps`, `container ls`, `inspect`, `logs`; logs default `--tail 200`, deny follow, cap tail
- `ping`: bounded count/deadline, deny flood
- `dmesg`: deny clear/follow/read-clear
- `hostname`: no args
- `ip`: deny mutating subcommands/options
- `find`: internal safe subset, paths anywhere, no exec/delete/file-writing actions
- content-reading tools deny obvious sensitive filenames: SSH private keys, `.env`, `.netrc`, `credentials*`, `*.pem`, `*.key`, `*.p12`, `*.pfx`; allow public keys like `*.pub`

## Config

JSON config under XDG config:

```text
$AIDUR_CONFIG
$XDG_CONFIG_HOME/aidur/config.json
~/.config/aidur/config.json
```

Initial Go config shape:

```json
{
  "model": ""
}
```

Environment overrides config where applicable.

## Test plan

Port Python behavior tests into Go unit tests for:

- CLI mode selection
- config path/model precedence
- request construction and redaction
- stdin one-shot default question
- tool allowlist/denylist
- every command-specific validator
- safe internal find
- secret redaction
- sensitive filename blocking
- chat slash command parsing
