# Aidur setup

Aidur is a minimal shell `dur` command. It sends your question to an OpenAI-compatible opencode endpoint and prints the answer in your terminal. It supports explicit clipboard context, optional persistent clipboard inclusion through OSC 52, piped stdin context, and ephemeral `dur chat` sessions with read-only diagnostic tools.

## Requirements

- macOS or Linux
- Any shell with a normal `PATH`
- Python 3 available as `python3`
- An opencode-compatible API key

No npm, pip, or package-manager dependencies are required.

## Install

From this repository, copy the executable script somewhere on your `PATH`:

```sh
mkdir -p ~/.local/bin
cp dur ~/.local/bin/dur
chmod +x ~/.local/bin/dur
```

Ensure `~/.local/bin` is on your `PATH`, then run:

```sh
dur help
```

## Configure

Set your API key:

```sh
export OPENCODE_ZEN_API_KEY="your-api-key"
```

In fish, use `set -Ux OPENCODE_ZEN_API_KEY "your-api-key"` if you want a universal variable.

Aidur defaults to this model:

```text
gpt-5.4-mini
```

To use a different model persistently, use `dur config model`:

```fish
dur models
dur config model gpt-5.5
```

You can also set `AIDUR_MODEL` as an environment override.

By default, Aidur sends requests to:

```text
https://opencode.ai/zen/v1/responses
```

If your key uses a different Responses-compatible base URL, set `OPENCODE_BASE_URL`:

```sh
export OPENCODE_BASE_URL="https://example.com"
```

Trailing slashes are okay. This also works:

```sh
export OPENCODE_BASE_URL="https://example.com/"
```

For an exact endpoint override, set `OPENCODE_ENDPOINT`:

```sh
export OPENCODE_ENDPOINT="https://example.com/v1/responses"
```

Optionally configure the request timeout in seconds, OSC 52 clipboard-read timeout in seconds, the maximum clipboard/stdin context bytes, read-only chat tool limits, and the config file path. `AIDUR_MODEL` still works as an environment override, but `dur config model` is preferred for persistent model selection.

```sh
export AIDUR_MODEL="gpt-5.5"
export AIDUR_TIMEOUT_SECONDS="60"
export AIDUR_CLIPBOARD_TIMEOUT_SECONDS="15"
export AIDUR_CLIP_MAX_BYTES="65536"
export AIDUR_TOOL_TIMEOUT_SECONDS="10"
export AIDUR_TOOL_MAX_BYTES="65536"
export AIDUR_CONFIG="$HOME/.config/aidur/config.json"
```

## Use

Show usage:

```fish
dur help
dur --help
```

Ask a question:

```fish
dur what does chmod 755 mean?
```

Start an ephemeral troubleshooting chat with read-only tools enabled:

```fish
dur chat
```

Chat state is kept in memory only. If Python's stdlib `readline` module is available, chat enables emacs-style line editing and in-memory input history. Use `/help`, `/tools`, `/pwd`, `/cd <path>`, `/tool N`, `/tool last`, `/models`, `/model <id>`, `/status`, `/streaming on|off`, `/exit`, and `/quit` inside the chat. Tool traces are one-line by default, for example `[tool 2] ls /etc`; `/tool 2` prints that tool call's command, stdout, and stderr. Tools run without a shell, with allowlisted commands, validators, time/output caps, and secret redaction. Chat mode currently uses non-streaming API responses so tool calls can be parsed safely.

Print the exact request body sent to the agent, including system prompt and user/context payload, to stderr:

```fish
dur --debug what does chmod 755 mean?
dur --debug --include-clipboard why did this fail?
```

Debug output redacts nothing from the request body itself, so it may include clipboard/stdin content. It does not print the API key or HTTP headers.

Include copied clipboard text as context explicitly for one request:

```fish
dur --include-clipboard why did this fail?
```

With no question, `dur --include-clipboard` asks Aidur to explain the clipboard content and suggest next steps:

```fish
dur --include-clipboard
```

Clipboard reads use OSC 52 through the controlling terminal. This works locally in Ghostty and can work over SSH back to Ghostty. Ghostty's `clipboard-read = ask|allow|deny` setting controls whether the terminal permits the read.

Manage models:

```fish
dur models
dur config model gpt-5.5
dur status
```

Streaming responses are enabled by default. Configure them with:

```fish
dur config streaming on
dur config streaming off
```

Aidur only lists and accepts GPT 5+ models that use the OpenCode Zen `/v1/responses` endpoint, and filters out `-pro` models. Model precedence is `AIDUR_MODEL` environment variable, then saved config model, then the default `gpt-5.4-mini`.

Configure optional persistent clipboard inclusion:

```fish
dur config include-clipboard never    # default: never auto-include clipboard
dur config include-clipboard ask      # ask before sending new clipboard content
dur config include-clipboard always   # send new clipboard content automatically
dur status
```

`dur status` shows the effective model, include-clipboard mode, streaming mode, and config path.

When `include-clipboard always` sends content automatically, Aidur prints this reminder to stderr:

```text
dur: sending clipboard data to agent
```

Pipe command output directly:

```fish
some-command 2>&1 | dur what happened?
```

When stdin is piped, Aidur includes stdin as untrusted context and skips persistent clipboard inclusion.

If persistent clipboard inclusion is enabled but no clipboard context is sent, Aidur includes a small `Clipboard status` block so the assistant knows clipboard context was attempted but unavailable, empty, declined, or already handled. If persistent clipboard inclusion is disabled, Aidur only includes this status when your question mentions the clipboard.

Example:

```text
chmod 755 gives the owner read/write/execute permissions, and gives group and others read/execute permissions...
```

You can ask practical terminal questions:

```fish
dur why did ssh say permission denied publickey?
dur explain rsync -avz --delete
dur how do I check disk usage safely?
```

## Troubleshooting

### `Usage: dur [--debug] [--include-clipboard] <question>`

You ran `dur` without a question. Use `dur --include-clipboard [question]` when you want to include clipboard context.

```fish
dur explain chmod 644
```

### `Missing OPENCODE_ZEN_API_KEY.`

Set your API key:

```sh
export OPENCODE_ZEN_API_KEY="your-api-key"
```

### Changing the model

Aidur uses `gpt-5.4-mini` by default. Set a persistent model:

```fish
dur models
dur config model gpt-5.5
```

`AIDUR_MODEL` remains an environment override:

```sh
export AIDUR_MODEL="gpt-5.5"
```

### `Invalid AIDUR_TIMEOUT_SECONDS: ...`

Set a positive numeric timeout:

```sh
export AIDUR_TIMEOUT_SECONDS="60"
```

### `dur: API returned HTTP ...`

The endpoint rejected the request or returned an error. Check:

- `OPENCODE_ZEN_API_KEY` is valid
- the effective model shown by `dur status` is valid for your provider
- `OPENCODE_BASE_URL` points to the Responses-compatible base URL, not the full `/v1/responses` URL
- Or use `OPENCODE_ENDPOINT` if you want to provide the exact full endpoint

### `dur: clipboard is empty`

You ran `dur --include-clipboard`, but the clipboard had no text.

### `dur: clipboard unavailable: ...`

Aidur could not read clipboard text. Check that your terminal supports OSC 52 clipboard reads and that Ghostty's `clipboard-read` setting is not `deny`. If you see the raw `ESC ] 52 ; ...` response printed in your shell after approving Ghostty's prompt, Aidur timed out before the terminal replied; either approve faster, set Ghostty `clipboard-read = allow`, or increase `AIDUR_CLIPBOARD_TIMEOUT_SECONDS`.

### Persistent clipboard inclusion sends stale content only once

Aidur remembers the MD5 fingerprint of the last automatically handled clipboard. Plain `dur <question>` will not repeatedly offer or send the same clipboard content. Explicit `dur --include-clipboard <question>` always attempts to send the current clipboard.

### `dur: request failed: ...`

Usually a network, DNS, TLS, or endpoint URL problem. Check your internet connection and `OPENCODE_BASE_URL`.

## Uninstall

Remove the installed files:

```sh
rm -f ~/.local/bin/dur
rm -f ~/.config/aidur/config.json
```

Optionally erase related environment variables:

```sh
unset OPENCODE_ZEN_API_KEY
unset AIDUR_MODEL
unset OPENCODE_BASE_URL
unset OPENCODE_ENDPOINT
unset AIDUR_TIMEOUT_SECONDS
unset AIDUR_CLIPBOARD_TIMEOUT_SECONDS
unset AIDUR_CLIP_MAX_BYTES
unset AIDUR_CONFIG
```

## Privacy and safety

For plain `dur <question>`, Aidur only sends the question you type unless persistent clipboard inclusion is enabled and the terminal permits a new clipboard read. For explicit `dur --include-clipboard [question]`, Aidur sends your question plus the current clipboard text. For piped stdin, Aidur sends your question plus stdin context. `dur chat` can read local/system information through its read-only tools and sends tool output to the API as part of the ephemeral conversation. `--debug` prints the request body to stderr for inspection and may expose clipboard/stdin/tool content locally. Streaming changes how the response is printed, not what context is sent. Aidur does not read shell history, automatically capture previous command output, make file changes, run a shell for the assistant, or keep persistent chat history.
