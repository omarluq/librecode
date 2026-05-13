<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/librecode-logo.svg">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/librecode-logo-light.svg">
    <img src="docs/assets/librecode-logo-light.svg" alt="librecode logo" width="560">
  </picture>
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/omarluq/librecode"><img src="https://img.shields.io/badge/reference-007d9c?style=flat&labelColor=24292e&logo=go&logoColor=white" alt="Go Reference"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-%3E%3D1.26-00ADD8?style=flat&labelColor=24292e&logo=go&logoColor=white" alt="Go Version"></a>
  <a href="https://github.com/omarluq/librecode/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/omarluq/librecode/ci.yml?style=flat&labelColor=24292e&label=CI&logo=github&logoColor=white" alt="CI"></a>
  <a href="https://github.com/omarluq/librecode/releases"><img src="https://img.shields.io/github/v/release/omarluq/librecode?style=flat&labelColor=24292e&color=28a745&label=Version&logo=semver&logoColor=white" alt="Version"></a>
  <a href="LICENSE.txt"><img src="https://img.shields.io/badge/License-MIT-blue?style=flat&labelColor=24292e&logo=opensourceinitiative&logoColor=white" alt="License: MIT"></a>
  <br/>
  <a href="https://codecov.io/gh/omarluq/librecode"><img src="https://img.shields.io/codecov/c/github/omarluq/librecode?style=flat&labelColor=24292e&logo=codecov&logoColor=white" alt="Codecov"></a>
  <a href="https://coderabbit.ai"><img src="https://img.shields.io/coderabbit/prs/github/omarluq/librecode?utm_source=oss&utm_medium=github&utm_campaign=omarluq%2Flibrecode&style=flat&labelColor=24292e&color=FF570A&label=CodeRabbit+Reviews" alt="CodeRabbit Reviews"></a>
  <a href="https://deepwiki.com/omarluq/librecode"><img src="https://deepwiki.com/badge.svg" alt="Ask DeepWiki"></a>
</p>

**librecode** is a free and open source terminal agent harness with a small core and room for extension. One binary. Terminal interface. Local sessions. Built-in tools. Provider auth by OAuth or API key. Lua at the edges.

<p align="center">
  <img src="docs/assets/librecode-introduce-yourself.gif" alt="librecode terminal demo" width="820">
</p>

## Features

- **Written in Go, not JavaScript** for a fast native CLI with straightforward binaries.
- **Interactive terminal chat** with streaming answers, visible reasoning blocks, chronological tool activity, scrollback, prompt history, app-owned mouse selection/copy, and a shimmering configurable working loader.
- **Persistent SQLite session trees** stored per user, with session listing, showing, branching metadata, and context rebuilding.
- **Built-in coding tools**: `read`, `write`, `edit`, `bash`, `grep`, `find`, and `ls` are available to the assistant and through the CLI.
- **Agent Skills support** loaded from project and user skill directories, with spec frontmatter support, autocomplete, explicit `/skill:<name>` loading, and conservative auto-activation.
- **Provider/model registry** with true OAuth for ChatGPT/Codex and Claude Pro/Max, API-key providers, transient provider retries, and custom model/provider definitions.
- **Trusted extensions** with Lua as the first runtime adapter for optional commands, tools, hooks, keymaps, runtime buffers, and low-level terminal events.
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

Built-in provider IDs:

| Provider                    | Auth                               | API family                  |
| --------------------------- | ---------------------------------- | --------------------------- |
| `openai-codex`              | Browser OAuth for ChatGPT Plus/Pro | Codex Responses             |
| `openai`                    | API key                            | OpenAI Responses            |
| `anthropic-claude`          | Browser OAuth for Claude Pro/Max   | Anthropic Messages          |
| `anthropic`                 | API key                            | Anthropic Messages          |
| `azure-openai-responses`    | API key/custom config              | OpenAI Responses            |
| OpenAI-compatible providers | API key                            | Chat Completions-compatible |

OpenAI-compatible built-ins: `cerebras`, `deepseek`, `groq`, `mistral`, `moonshotai`, `moonshotai-cn`, `openrouter`, `vercel-ai-gateway`, `xai`, and `zai`.

The default assistant config is:

```yaml
assistant:
  provider: openai-codex
  model: gpt-5.5
  thinking_level: off
  retry:
    enabled: true
    max_attempts: 3
    base_delay: 2s
    max_delay: 30s
```

Built-in providers are intentionally limited to API families librecode implements directly: OpenAI/Codex Responses, OpenAI-compatible chat completions, and Anthropic Messages. Additional providers can still be added through custom model/provider definitions.

Credentials can come from:

- `/login openai-codex` for ChatGPT/Codex subscription OAuth;
- `/login anthropic-claude` for Claude Pro/Max OAuth;
- `/login <provider> <api-key>` for API-key providers such as `anthropic`, `openai`, `openrouter`, or `zai`;
- provider-specific environment variables such as `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `ANTHROPIC_OAUTH_TOKEN`, `OPENROUTER_API_KEY`, or `ZAI_API_KEY`;
- custom provider config stored in the runtime model document.

Provider IDs are configured with `LIBRECODE_ASSISTANT_PROVIDER` or `assistant.provider`; model IDs use `LIBRECODE_ASSISTANT_MODEL` or `assistant.model`.

## Configuration

librecode resolves config in this order:

1. `--config <path>`
2. `LIBRECODE_*` environment variables
3. `./.librecode/config.yaml`
4. `~/.librecode/config.yaml` or `$LIBRECODE_HOME/config.yaml`
5. built-in defaults

Useful commands:

```bash
librecode config show
librecode config validate
```

See [`config.example.yaml`](config.example.yaml) for all current config keys. The in-progress loader text defaults to `Shenaniganing...` and is configurable with `app.working_loader.text`.

Built-in memory limits protect untrusted input and remote bodies: prompt stdin and tool JSON stdin are capped at 1 MiB, provider response/error bodies at 16 MiB, and ksqlDB response bodies at 8 MiB.

Default global persistence lives under one librecode home:

- librecode home: `~/.librecode` or `$LIBRECODE_HOME`
- config: `~/.librecode/config.yaml`
- sessions database: `~/.librecode/librecode.db`
- auth storage: `~/.librecode/auth.json`

Project-local overrides live under `./.librecode/` too. If `./.librecode/auth.json` or `./.librecode/librecode.db` exists, librecode uses it instead of the global file for that project.

## Skills

Skills are Agent Skills-compatible directories containing `SKILL.md`. Skill metadata is always advertised to the model, and matching skills can be auto-activated by loading their full `SKILL.md` into the request context.

Default skill roots, in priority order:

1. `./.librecode/skills`
2. `./.agents/skills`
3. `~/.librecode/skills`
4. `~/.agents/skills`

Duplicate skill names are deduped by priority, so project-local `.librecode` skills win over project `.agents` and user-global skills. Discovery honors `.gitignore`, `.ignore`, and `.fdignore` files inside skill roots.

A minimal skill looks like:

```markdown
---
name: my-skill
description: Use when working on my project-specific workflow.
license: MIT
compatibility: Works with librecode and Agent Skills-compatible agents.
allowed-tools: Read Bash(git:*)
metadata:
  author: me
---

Follow these project-specific instructions...
```

Useful commands:

```bash
librecode skill list
librecode skill show my-skill
librecode skill validate
```

Inside chat, `/skill` lists discovered skills. Use `/skill my-skill` or `/skill:my-skill` to load a skill through the read-tool path and render a `loaded skill my-skill` block. User-invocable skills also appear in slash autocomplete as `/skill:<name>`.

## Extensions

Extensions are trusted local code. librecode follows the Unix philosophy here: extensions are powerful, low-level, and allowed to footgun if you ask them to. Lua is the first supported extension runtime; the host is designed so additional runtimes can be added later.

Default extension roots come from `extensions.paths` in config, defaulting to:

- `.librecode/extensions`

The default chat UI is Go-owned and extensions are optional customization. Use `--no-extensions` to disable configured extensions for a single run.

Current extension capabilities include:

- registering commands and extension-backed tools;
- listening to low-level runtime events;
- intercepting key input with priorities;
- reading and mutating runtime buffers such as `composer`, `status`, `transcript`, `thinking`, and `tools`;
- creating namespaces, autocmds, and keymaps through a Neovim-inspired Lua API.

Architecture note: Go owns the polished default chat experience and hot rendering paths. Lua remains a trusted optional extension layer for keymaps, commands, hooks, small overlays, and custom workflows.

For architecture, roadmap, and API details, see:

- [`docs/adr/0001-programmable-runtime.md`](docs/adr/0001-programmable-runtime.md)
- [`docs/runtime-architecture.md`](docs/runtime-architecture.md)
- [`docs/extension-runtime.md`](docs/extension-runtime.md)
- [`docs/extension-roadmap.md`](docs/extension-roadmap.md)
- [`docs/extension-api.md`](docs/extension-api.md)
- [`docs/rendering-boundary.md`](docs/rendering-boundary.md)
- [`docs/skills.md`](docs/skills.md)

Inspect loaded extensions:

```bash
librecode extension list
librecode extension run <command> [args...]
```

## Built-in tools and trust boundaries

The built-in tools are intentionally powerful. `bash` can execute commands, and file tools can read or mutate paths you provide. This is a local coding assistant, not a sandbox.

On Windows, the `bash` tool requires a real Bash shell rather than `cmd.exe`. librecode checks `LIBRECODE_BASH_PATH`, common Git Bash install paths, then `bash.exe` on `PATH`. Native Windows users should install Git Bash/MSYS2/Cygwin or run librecode from WSL.

Available tools:

| Tool    | Mutates files? | Purpose                                            |
| ------- | -------------- | -------------------------------------------------- |
| `read`  | No             | Read text/image files with truncation controls.    |
| `ls`    | No             | List directory entries.                            |
| `find`  | No             | Search file paths by glob.                         |
| `grep`  | No             | Search file contents.                              |
| `write` | Yes            | Overwrite/create files.                            |
| `edit`  | Yes            | Exact text replacement with uniqueness checks.     |
| `bash`  | Yes            | Execute shell commands with timeout/output limits. |

Only run librecode in workspaces where you trust the model, tools, and extensions you enable.

## CLI reference

```text
librecode [--resume] [--config path] [--no-extensions]
librecode chat [--resume | --session id]
librecode prompt [--resume | --session id | --name name] [message]
librecode session new [name]
librecode session list
librecode session show <session-id>
librecode skill list
librecode skill show <name>
librecode skill validate
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
task fmt          # auto-format and auto-fix lint issues
task fmt-check    # check formatting without modifying files
task ci           # fmt-check + lint + test + build, non-mutating
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
internal/assistant/     Prompt orchestration, provider calls, tool loop, cache integration
internal/auth/          Provider credential storage, OAuth flows, and token refresh
internal/config/        Viper config defaults, loading, and validation
internal/core/          Resources: system prompts, context files, skills, slash prompts
internal/database/      SQLite repositories, migrations, ksqlDB client
internal/di/            Service wiring with samber/do
internal/extension/     Extension host and Lua runtime API bridge
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

<a href="https://sonarcloud.io/summary/new_code?id=omarluq_librecode"><img src="https://sonarcloud.io/images/project_badges/sonarcloud-dark.svg" alt="SonarCloud Quality Gate"></a>
