<img width="100%"  alt="b634d5e1-10f3-4de5-9534-310ca5529641" src="https://github.com/user-attachments/assets/a6215996-02bf-4413-a1df-b3beba2e63e6" />

# AI-dur

AI-dur (`dur`) is a small terminal assistant for macOS and Linux. It can answer one-shot questions, manage chat sessions with an agent, and optionally use a set of read-only diagnostic tools while chatting.

I spend a lot of time debugging servers, sometimes quite old ones with legacy systems and hand-rolled configuration. AI assistance is very valuable on these servers, but the risk of an agent making an unnoticed change is unacceptable. AI-dur provides a set of read-only tools to an agent to help you explore and diagnose problems.

AI-dur is a single binary with no external dependencies. Just copy it wherever you need it, set an environment variable, and you're ready to go.

## Install

Grab Darwin arm64 or Linux amd64 binaries from the releases page or build from source:

```sh
go build -o dur ./cmd/dur
# or
make build
```

Then put `dur` somewhere on your `PATH`.

## Configuration

Aidur uses the OpenCode Zen Responses API. Set your API key:

```sh
export OPENCODE_ZEN_API_KEY="..."
```

Persistent config is stored at:

```text
~/.config/aidur/config.json
```

Override the config path with:

```sh
export AIDUR_CONFIG="$HOME/.config/aidur/config.json"
```

Useful environment overrides:

```sh
export AIDUR_MODEL="gpt-5.4-mini"
export AIDUR_THINKING="medium"   # off, low, medium, high
export AIDUR_AGENT_NAME="aidur"
```

## Usage

Start chat:

```sh
dur
```

Ask a one-shot question:

```sh
dur explain this error
```

Use a model for this invocation only:

```sh
dur --model gpt-5.4-mini explain this error
```

List available models:

```sh
dur --models
```

Pipe stdin into a one-shot question:

```sh
echo "blah" | dur what does this mean
```

Pipe stdin into chat as context:

```sh
echo "blah" | dur
```

Use read-only tools in one-shot mode:

```sh
dur --tools on what is using disk space here?
```

## Chat commands

Inside chat:

```text
/help                              show help
/model <id>                        set persistent model
/models                            list available models
/name <agent-name>                 set assistant prompt name
/status                            show configuration
/thinking off|low|medium|high      set reasoning effort
/tools                             list available read-only tools
/tools verbose on|off              toggle expanded tool output
/tools history                     list tool call history
/tool N                            show tool call N
/tool last                         show most recent tool call
/cd <path>                         change tool working directory
/debug on|off                      toggle debug request output
/quit                              exit
! ls /etc                          run command so agent can see results
```

## Sudo

If you want to run as root, you can preserve your environment using sudo:

```sh
sudo -E dur ...
```

or set `AIDUR_CONFIG` explicitly:

```sh
sudo AIDUR_CONFIG="$HOME/.config/aidur/config.json" dur ...
```

## Notes

- Chat history is in-memory only, no session data is stored on disk.
- Tool execution is read-only, shell-free, bounded, and intended for diagnostics.
- `--model` changes the model only for that invocation. Use `/model <id>` in chat to persist a default.
