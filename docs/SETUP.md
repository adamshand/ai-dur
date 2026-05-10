# Aidur setup

Aidur is a minimal shell `ai` command. It sends your question to an OpenAI-compatible opencode endpoint and prints the answer in your terminal. It supports explicit clipboard context, optional auto-clipboard context through OSC 52, and piped stdin context.

## Requirements

- macOS or Linux
- fish or bash shell
- Python 3 available as `python3`
- An opencode-compatible API key

No npm, pip, or package-manager dependencies are required.

## Install

From this repository, copy the Python script and shell function into the expected locations.

Fish:

```sh
mkdir -p ~/.local/bin ~/.config/fish/conf.d
cp bin/aidur.py ~/.local/bin/aidur.py
cp fish/conf.d/aidur.fish ~/.config/fish/conf.d/aidur.fish
chmod +x ~/.local/bin/aidur.py
```

Restart fish, or load the function immediately:

```fish
source ~/.config/fish/conf.d/aidur.fish
```

Bash:

```sh
mkdir -p ~/.local/bin
cp bin/aidur.py ~/.local/bin/aidur.py
chmod +x ~/.local/bin/aidur.py
cat bash/aidur.bash >> ~/.bashrc
```

Restart bash, or load the function immediately:

```sh
source ~/.bashrc
```

If you install the Python script somewhere else, set `AIDUR_SCRIPT` to that path.

## Configure

Set your API key:

```fish
set -Ux OPENCODE_ZEN_API_KEY "your-api-key"
```

Aidur defaults to this model:

```text
gpt-5.4-mini
```

To use a different model persistently, use `ai models set`:

```fish
ai models list
ai models set gpt-5.5
```

You can also set `AIDUR_MODEL` as an environment override.

By default, Aidur sends requests to:

```text
https://opencode.ai/zen/v1/responses
```

If your key uses a different Responses-compatible base URL, set `OPENCODE_BASE_URL`:

```fish
set -Ux OPENCODE_BASE_URL "https://example.com"
```

Trailing slashes are okay. This also works:

```fish
set -Ux OPENCODE_BASE_URL "https://example.com/"
```

For an exact endpoint override, set `OPENCODE_ENDPOINT`:

```fish
set -Ux OPENCODE_ENDPOINT "https://example.com/v1/responses"
```

Optionally configure the request timeout in seconds, the maximum clipboard/stdin context bytes, and the config file path. `AIDUR_MODEL` still works as an environment override, but `ai models set` is preferred for persistent model selection.

```fish
set -Ux AIDUR_MODEL "gpt-5.5"
set -Ux AIDUR_TIMEOUT_SECONDS "60"
set -Ux AIDUR_CLIP_MAX_BYTES "65536"
set -Ux AIDUR_CONFIG "~/.config/aidur/config.json"
```

## Use

Show usage:

```fish
ai help
ai --help
```

Ask a question:

```fish
ai what does chmod 755 mean?
```

Include copied clipboard text as context explicitly:

```fish
ai clip why did this fail?
```

With no question, `ai clip` asks Aidur to explain the clipboard content and suggest next steps:

```fish
ai clip
```

Clipboard reads use OSC 52 through the controlling terminal. This works locally in Ghostty and can work over SSH back to Ghostty. Ghostty's `clipboard-read = ask|allow|deny` setting controls whether the terminal permits the read.

Manage models:

```fish
ai models list
ai models set gpt-5.5
ai status
```

Aidur only lists and accepts GPT 5+ models that use the OpenCode Zen `/v1/responses` endpoint, and filters out `-pro` models. Model precedence is `AIDUR_MODEL` environment variable, then saved config model, then the default `gpt-5.4-mini`.

Configure optional auto-clipboard:

```fish
ai auto-clip off     # default: never auto-include clipboard
ai auto-clip ask     # ask before sending new clipboard content
ai auto-clip on      # send new clipboard content automatically
ai status
```

When `auto-clip on` sends content automatically, Aidur prints this reminder to stderr:

```text
ai: sending clipboard data to agent
```

Pipe command output directly:

```fish
some-command 2>&1 | ai what happened?
```

When stdin is piped, Aidur includes stdin as untrusted context and skips auto-clipboard.

Example:

```text
chmod 755 gives the owner read/write/execute permissions, and gives group and others read/execute permissions...
```

You can ask practical terminal questions:

```fish
ai why did ssh say permission denied publickey?
ai explain rsync -avz --delete
ai how do I check disk usage safely?
```

## Troubleshooting

### `Usage: ai <question>`

You ran `ai` without a question. Use `ai clip [question]` when you want to include clipboard context.

```fish
ai explain chmod 644
```

### `Missing OPENCODE_ZEN_API_KEY.`

Set your API key:

```fish
set -Ux OPENCODE_ZEN_API_KEY "your-api-key"
```

### Changing the model

Aidur uses `gpt-5.4-mini` by default. Set a persistent model:

```fish
ai models list
ai models set gpt-5.5
```

`AIDUR_MODEL` remains an environment override:

```fish
set -Ux AIDUR_MODEL "gpt-5.5"
```

### `Invalid AIDUR_TIMEOUT_SECONDS: ...`

Set a positive numeric timeout:

```fish
set -Ux AIDUR_TIMEOUT_SECONDS "60"
```

### `ai: API returned HTTP ...`

The endpoint rejected the request or returned an error. Check:

- `OPENCODE_ZEN_API_KEY` is valid
- the effective model shown by `ai status` is valid for your provider
- `OPENCODE_BASE_URL` points to the Responses-compatible base URL, not the full `/v1/responses` URL
- Or use `OPENCODE_ENDPOINT` if you want to provide the exact full endpoint

### `ai: clipboard is empty`

You ran `ai clip`, but the clipboard had no text.

### `ai: clipboard unavailable: ...`

Aidur could not read clipboard text. Check that your terminal supports OSC 52 clipboard reads and that Ghostty's `clipboard-read` setting is not `deny`.

### Auto-clipboard sends stale content only once

Aidur remembers the MD5 fingerprint of the last automatically handled clipboard. Plain `ai <question>` will not repeatedly offer or send the same clipboard content. Explicit `ai clip <question>` always attempts to send the current clipboard.

### `ai: request failed: ...`

Usually a network, DNS, TLS, or endpoint URL problem. Check your internet connection and `OPENCODE_BASE_URL`.

## Uninstall

Remove the installed files:

```sh
rm -f ~/.local/bin/aidur.py
rm -f ~/.config/fish/conf.d/aidur.fish
rm -f ~/.config/aidur/config.json
```

Optionally erase the universal fish variables:

```fish
set -e OPENCODE_ZEN_API_KEY
set -e AIDUR_MODEL
set -e OPENCODE_BASE_URL
set -e OPENCODE_ENDPOINT
set -e AIDUR_TIMEOUT_SECONDS
set -e AIDUR_CLIP_MAX_BYTES
set -e AIDUR_CONFIG
set -e AIDUR_SCRIPT
```

## Privacy and safety

For plain `ai <question>`, Aidur only sends the question you type unless auto-clipboard is enabled and the terminal permits a new clipboard read. For explicit `ai clip [question]`, Aidur sends your question plus the current clipboard text. For piped stdin, Aidur sends your question plus stdin context. It does not read shell history, automatically capture previous command output, read files, execute assistant-requested commands, or keep persistent chat history.
