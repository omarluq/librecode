# librecode

<p align="center">
  <a href="https://pkg.go.dev/github.com/omarluq/librecode"><img src="https://img.shields.io/badge/reference-007d9c?style=flat&labelColor=24292e&logo=go&logoColor=white" alt="Go Reference"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-%3E%3D1.26-00ADD8?style=flat&labelColor=24292e&logo=go&logoColor=white" alt="Go Version"></a>
  <a href="https://github.com/omarluq/librecode/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/omarluq/librecode/ci.yml?style=flat&labelColor=24292e&label=CI&logo=github&logoColor=white" alt="CI"></a>
  <a href="https://github.com/omarluq/librecode/releases"><img src="https://img.shields.io/github/v/release/omarluq/librecode?style=flat&labelColor=24292e&color=28a745&label=Version&logo=semver&logoColor=white" alt="Version"></a>
  <a href="LICENSE.txt"><img src="https://img.shields.io/badge/License-MIT-blue?style=flat&labelColor=24292e&logo=opensourceinitiative&logoColor=white" alt="License: MIT"></a>
</p>

**librecode** is a local-first AI coding assistant and programmable terminal runtime written in Go.
It combines an interactive chat UI, persistent sessions, built-in coding tools, Agent Skills-compatible instructions, and trusted Lua extensions that can reach deep into the runtime.

## Features

- **Interactive terminal chat** with streaming answers, visible reasoning blocks, chronological tool activity, scrollback, prompt history, and optional Vim-style composer behavior.
- **Fresh sessions by default**: `librecode` starts a new session; use `--resume` to reopen the latest session for the current working directory.
- **Persistent SQLite session trees** stored per user, with session listing, showing, branching metadata, and context rebuilding.
- **Built-in coding tools**: `read`, `write`, `edit`, `bash`, `grep`, `find`, and `ls` are available to the assistant and through the CLI.
- **Agent Skills support** loaded from project and user skill directories, with deterministic priority and dedupe.
- **Trusted Lua extensions** for commands, tools, hooks, keymaps, runtime buffers, and low-level terminal events.
- **Provider/model registry** with OpenAI/Codex Responses, OpenAI-compatible chat completions, Anthropic Messages, and custom model/provider definitions.
- **Configuration via YAML + env vars** with sane defaults and strict validation.

## Install

### From source

```bash
git clone https://github.com/omarluq/librecode.git
cd librecode
mise install          # optional: install the pinned Go/Task/golangci-lint/lefthook versions
task build            # writes ./bin/librecode
./bin/librecode --help
```

If you do not use `mise`, install a Go toolchain matching `go.mod` and [Task](https://taskfile.dev/) yourself.

### With `go install`

```bash
go install github.com/omarluq/librecode/cmd/librecode@latest
```

## Quick start

Open a fresh interactive chat session:

```bash
librecode
```

Resume the latest session for the current working directory:

```bash
librecode --resume
# or
librecode chat --resume
```

Send a one-shot prompt:

```bash
librecode prompt "summarize this repo"
librecode prompt --resume "continue from the last session"
```

Run a built-in tool directly:

```bash
librecode tool list
librecode tool run read '{"path":"README.md"}'
librecode tool run bash '{"command":"go test ./...","timeout":120}'
```

Manage sessions:

```bash
librecode session new "docs pass"
librecode session list
librecode session show <session-id>
```

## Authentication and providers

The default assistant config is:

```yaml
assistant:
  provider: openai-codex
  model: gpt-5.5
  thinking_level: off
```

Credentials can come from:

- provider-specific environment variables such as `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`, or `ZAI_API_KEY`;
- custom provider config stored in the runtime model document;
- compatible existing Codex auth files imported from `~/.codex/auth.json` or `~/.pi/agent/auth.json`.

Provider IDs are configured with `LIBRECODE_ASSISTANT_PROVIDER` or `assistant.provider`; model IDs use `LIBRECODE_ASSISTANT_MODEL` or `assistant.model`.

## Configuration

librecode resolves config in this order:

1. `--config <path>`
2. `LIBRECODE_*` environment variables
3. `./config.yaml`
4. `$HOME/.config/librecode/config.yaml`
5. built-in defaults

Useful commands:

```bash
librecode config show
librecode config validate
```

See [`config.example.yaml`](config.example.yaml) for all current config keys.

Default persistence paths:

- sessions database: `~/.local/state/librecode/sessions.db`
- auth storage: `$XDG_CONFIG_HOME/librecode/auth.json` or `~/.config/librecode/auth.json`

## Skills

Skills are Agent Skills-compatible directories or Markdown files with frontmatter. They are injected into the assistant prompt when their metadata is valid.

Default skill roots, in priority order:

1. `./.librecode/skills`
2. `./.agents/skills`
3. `~/.librecode/skills`
4. `~/.agents/skills`

Duplicate skill names are deduped by priority, so project-local `.librecode` skills win over project `.agents` and user-global skills.

A minimal skill looks like:

```markdown
---
name: my-skill
description: Use when working on my project-specific workflow.
---

Follow these project-specific instructions...
```

## Extensions

Extensions are trusted local Lua code. librecode follows the Unix philosophy here: extensions are powerful, low-level, and allowed to footgun if you ask them to.

Default extension roots:

1. `extensions/` — official bundled extensions
2. paths from `extensions.paths` in config, defaulting to:
   - `extensions`
   - `.librecode/extensions`

Current extension capabilities include:

- registering commands and extension-backed tools;
- listening to low-level runtime events;
- intercepting key input with priorities;
- reading and mutating runtime buffers such as `composer`, `status`, `transcript`, `thinking`, and `tools`;
- creating namespaces, autocmds, and keymaps through a Neovim-inspired Lua API.

The bundled [`extensions/vim-mode.lua`](extensions/vim-mode.lua) demonstrates composer control through the public Lua runtime API.

For architecture, roadmap, and API details, see:

- [`docs/adr/0001-programmable-runtime.md`](docs/adr/0001-programmable-runtime.md)
- [`docs/runtime-architecture.md`](docs/runtime-architecture.md)
- [`docs/extension-runtime.md`](docs/extension-runtime.md)
- [`docs/extension-roadmap.md`](docs/extension-roadmap.md)
- [`docs/extension-api.md`](docs/extension-api.md)

Inspect loaded extensions:

```bash
librecode extension list
librecode extension run <command> [args...]
```

## Built-in tools and trust boundaries

The built-in tools are intentionally powerful. `bash` can execute commands, and file tools can read or mutate paths you provide. This is a local coding assistant, not a sandbox.

Available tools:

| Tool | Mutates files? | Purpose |
| --- | --- | --- |
| `read` | No | Read text/image files with truncation controls. |
| `ls` | No | List directory entries. |
| `find` | No | Search file paths by glob. |
| `grep` | No | Search file contents. |
| `write` | Yes | Overwrite/create files. |
| `edit` | Yes | Exact text replacement with uniqueness checks. |
| `bash` | Yes | Execute shell commands with timeout/output limits. |

Only run librecode in workspaces where you trust the model, tools, and extensions you enable.

## CLI reference

```text
librecode [--resume] [--config path]
librecode chat [--resume | --session id]
librecode prompt [--resume | --session id | --name name] [message]
librecode session new [name]
librecode session list
librecode session show <session-id>
librecode tool list
librecode tool run <name> [json-args|-] [--cwd path]
librecode extension list
librecode extension run <command> [args...]
librecode config show
librecode config validate
librecode migrate
librecode version
```

Use `librecode <command> --help` for exact flags and subcommands.

## Development

```bash
task              # list tasks
task build        # build ./bin/librecode
task run          # build and run
task test         # go test -race ./...
task test-short   # short race-enabled tests
task test-coverage # coverage.out + coverage.html
task lint         # golangci-lint run
task fmt          # golangci-lint run --fix
task ci           # fmt + lint + test + build
task tidy         # go mod tidy
task clean        # remove build/test/cache artifacts
```

Project-local caches are used for reproducible local runs and are gitignored:

- `.gocache/`
- `.gomodcache/`
- `.tmp/`

## Project layout

```text
cmd/librecode/          CLI commands and process entrypoint
extensions/             Official bundled Lua extensions
internal/assistant/     Prompt orchestration, provider calls, tool loop, cache integration
internal/auth/          Provider credential storage and Codex auth import/refresh
internal/config/        Viper config defaults, loading, and validation
internal/core/          Resources: system prompts, context files, skills, slash prompts
internal/database/      SQLite repositories, migrations, ksqlDB client
internal/di/            Service wiring with samber/do
internal/extension/     Lua extension manager and runtime API bridge
internal/model/         Provider/model registry and auth resolution
internal/terminal/      Interactive terminal UI
internal/tool/          Built-in coding tools
internal/vinfo/         Version metadata injected at build time
```

## Release

Releases are built by GoReleaser from `v*.*.*` tags:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release workflow cross-compiles Linux, macOS, and Windows binaries, archives them, generates checksums, and publishes a GitHub release.

## License

MIT — see [`LICENSE.txt`](LICENSE.txt).
