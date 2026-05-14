<img width="100%" alt="image" src="https://github.com/user-attachments/assets/9bd3eb6f-4045-4a8a-925c-81f3ba6dab69" />

# AI-dur

AI-dur (`dur`) is a small terminal assistant for macOS and Linux. It can answer one-shot questions, manage chat sessions with an agent, and optionally use a set of read-only diagnostic tools while chatting.

I spend a lot of time debugging servers, sometimes quite old ones with legacy systems and hand-rolled configuration. AI assistance is very valuable on these servers, but the risk of an agent making an unnoticed change is unacceptable. AI-dur provides a set of read-only tools to an agent to help you explore and diagnose problems.

AI-dur is a single binary with no external dependencies. Just copy it wherever you need it, set an environment variable, and you're ready to go.

## Install

Grab the matching macOS or Linux release archive for your CPU architecture (`amd64` or `arm64`) from the releases page, or build from source:

```sh
go build -o dur ./cmd/dur
# or
make build
```

Then put `dur` somewhere on your `PATH`.

## Configuration

Aidur uses the OpenCode Zen Responses API. Save your API key to config:

```sh
dur auth
```

Or set it with an environment variable:

```sh
export OPENCODE_ZEN_API_KEY="..."
```

`OPENCODE_ZEN_API_KEY` takes precedence over the config file.

Persistent config is stored at:

```text
~/.config/aidur/config.json
```

`dur` writes the config directory as `0700` and the config file as `0600`.

Override the config path with:

```sh
export AIDUR_CONFIG="$HOME/.config/aidur/config.json"
```

Useful environment overrides:

```sh
export AIDUR_MODEL="gpt-5.4-mini"
export AIDUR_THINKING="medium"   # off, low, medium, high
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

Save your API key to config:

```sh
dur auth
```

List available models:

```sh
dur --models
```

Update to the latest GitHub release:

```sh
dur --update
```

`dur --update` downloads the matching release archive for your platform, verifies it against `checksums.txt`, and replaces the current binary location when writable. It is disabled for `dev` builds.

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
/instructions                      show custom instructions
/instructions <text>               replace custom instructions
/instructions clear                remove custom instructions
/model <id>                        set persistent model
/models                            list available models
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
