# AGENTS.md

Guidance for coding agents working in this repository.

## Project overview

AI-dur (`dur`) is a small Go terminal assistant for macOS and Linux. It supports one-shot questions, interactive chat, and a bounded set of read-only diagnostic tools for safe server debugging.

## Repository layout

- `cmd/dur/` - CLI entry point and top-level command handling.
- `internal/chat/` - interactive terminal chat UI and session behavior.
- `internal/config/` - config loading and environment overrides.
- `internal/provider/` - OpenCode Zen Responses API client and request/response types.
- `internal/tools/` - read-only tool runner, validation, redaction, and safety checks.
- `docs/` - design notes, setup, and rewrite/spec documentation.
- `dist/`, `dur.darwin`, `dur.linux`, `dur` - generated build artifacts; do not edit directly.

## Commands

Run these from the repository root:

```sh
go test ./...
make build
make dist
```

Use `go test ./...` before handing off code changes. Use `make build` when changing CLI startup, linker version wiring, or build-related behavior. `make dist` creates release archives and should only be used when release artifacts are needed.

## Code style

- Follow idiomatic Go and run `gofmt` on changed Go files.
- Keep behavior explicit and easy to debug; prefer clear `if`/`switch` logic over clever compact expressions.
- Return actionable errors with the `dur:` prefix at CLI boundaries where existing code does so.
- Keep package boundaries simple: CLI orchestration in `cmd/dur`, terminal/session behavior in `internal/chat`, API behavior in `internal/provider`, and tool safety in `internal/tools`.
- Avoid introducing new dependencies unless they clearly reduce complexity and are appropriate for a single-binary CLI.
- Add or update tests alongside behavior changes, especially for config resolution, provider parsing, and tool validation.

## Safety and behavior constraints

- Preserve the read-only guarantee of diagnostic tools. Do not add mutating commands or shell execution paths to `internal/tools`.
- Keep tool execution bounded by timeout, output limits, allowlists, path validation, and redaction.
- Do not log or expose API keys, config secrets, or unredacted sensitive command output.
- Chat history is intended to remain in-memory only unless a change explicitly designs persistent storage.
- Avoid changing documented CLI flags, environment variables, chat commands, or output formats without updating `README.md` and relevant tests.

## Documentation

- Update `README.md` when user-facing commands, flags, environment variables, install steps, or behavioral guarantees change.
- Keep documentation concise and copy-pasteable; shell snippets should be valid shell.
