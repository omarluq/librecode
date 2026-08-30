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
  <a href="https://goreportcard.com/report/github.com/omarluq/librecode"><img src="https://img.shields.io/badge/report-A%2B-00ADD8?style=flat&labelColor=24292e&logo=go&logoColor=white" alt="Go Report Card"></a>
  <a href="https://github.com/omarluq/librecode/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/omarluq/librecode/ci.yml?style=flat&labelColor=24292e&label=CI&logo=github&logoColor=white" alt="CI"></a>
  <!--  <a href="https://github.com/omarluq/librecode/releases"><img src="https://img.shields.io/github/v/release/omarluq/librecode?style=flat&labelColor=24292e&color=28a745&label=Version&logo=semver&logoColor=white" alt="Version"></a> -->
  </br>
  <a href="LICENSE.txt"><img src="https://img.shields.io/badge/License-MIT-blue?style=flat&labelColor=24292e&logo=opensourceinitiative&logoColor=white" alt="License: MIT"></a>
  <a href="https://codecov.io/gh/omarluq/librecode"><img src="https://img.shields.io/codecov/c/github/omarluq/librecode?style=flat&labelColor=24292e&logo=codecov&logoColor=white" alt="Codecov"></a>
  <a href="https://coderabbit.ai"><img src="https://img.shields.io/coderabbit/prs/github/omarluq/librecode?utm_source=oss&utm_medium=github&utm_campaign=omarluq%2Flibrecode&style=flat&labelColor=24292e&color=FF570A&label=CodeRabbit+Reviews" alt="CodeRabbit Reviews"></a>
  <a href="https://deepwiki.com/omarluq/librecode"><img src="https://deepwiki.com/badge.svg" alt="Ask DeepWiki"></a>
</p>

<p align="center">
  <strong>librecode is a small, local-first terminal AI agent.</strong>
  <br><br>
  No sandbox. No MCP. No permission prompts. No marketplace. No telemetry.
  <br><br>
  Just a model, your shell, and a focused set of tools that do what they say, designed for developers who'd rather review their own diffs than click through approval dialogs.
</p>

<p align="center">
  <img src="docs/assets/librecode-fibonacci-demo.gif" alt="librecode terminal demo" width="820">
</p>

> [!IMPORTANT]
> librecode is pre-release software. Expect bugs, rough edges, breaking changes, half-finished surfaces, and the occasional crash. APIs, config keys, and on-disk formats may shift without notice until 1.0. If you need stability, wait. If you want to help shape it, jump in: issues and PRs welcome.

## Philosophy

The agent CLI space is moving toward more layers: permission models, sandboxed tool servers, plugin marketplaces, extension protocols. Those are reasonable choices for some teams. librecode is the smaller, simpler alternative for people who want fewer of them.

- **No MCP.** Built-in tools plus optional Lua extensions are the entire surface area. Fewer moving parts, no separate servers to manage.
- **No sandbox.** Tools run with your permissions, in your shell, against your files. You decide what to run librecode against.
- **No permission prompts.** The agent acts within the project you launched it in, without interrupting to confirm each step.
- **Bring your own model.** OAuth into ChatGPT/Codex or Claude Pro/Max, use an API key for anything OpenAI-compatible, or define your own provider.
- **One binary.** Static Go. No Node, no Python venv, no Electron. Fast cold start, no background services.
- **Local everything.** Sessions in a SQLite file, auth in a JSON file. Project-local `.librecode/` keeps state scoped to the repo.

It's a direct, capable tool. Treat it like one.

## What's in the box

- **Interactive terminal chat**: streaming text and reasoning, chronological tool activity, scrollback, prompt history, mouse selection, image attachments, model/thinking controls, and searchable panels.
- **Focused coding tools**: filesystem reads and writes, shell execution, text and path search, structural syntax-tree inspection, and explicit URL fetching.
- **Tool orchestration**: direct calls, durable background tool execution, and an `execute` code mode that can discover and compose tools from small Go programs.
- **Durable subagents and workflows**: delegate independent work to isolated child sessions or launch dynamic, asynchronous multi-agent workflows that outlive the initiating model turn.
- **Persistent SQLite sessions**: resumable conversation trees with naming, cloning, forking, compaction, per-session settings, and full transcript history.
- **Context awareness**: token budgeting, contributor breakdowns, automatic/manual compaction, and model-aware context/output limits.
- **Agent Skills and instructions**: discover project/user `SKILL.md` bundles and layered `AGENTS.md` instructions, with progressive disclosure and slash-command activation.
- **Provider/model registry**: OAuth for ChatGPT/Codex and Claude Pro/Max, API-key providers, models.dev metadata discovery, scoped model lists, and transient-failure retries.
- **Lua extensions**: optional trusted-local commands, tools, keymaps, buffers, renderers, timers, and lifecycle/tool/context hooks. The default UI remains Go-owned.
- **YAML config + env vars**: layered configuration, strict validation, project-local overrides, and no background daemon.

## What to expect

librecode executes shell commands and edits files as the model requests them, without per-action confirmation prompts. That's the design: it keeps the loop fast and the surface area small, but it's worth being explicit about.

A few suggestions for using it comfortably:

- Run it in workspaces you own, ideally under version control, so changes are easy to review and revert.
- Use models you've found reliable for the kind of work you're asking for.
- Treat a librecode session like running a script from an unfamiliar repo: fine when you've decided to trust it, worth pausing on when you haven't.

If you'd prefer per-action approval, sandboxing, or policy enforcement, other agent CLIs offer that and may suit you better.

## Install

### From source

```bash
git clone https://github.com/omarluq/librecode.git
cd librecode
vfox install --all                            # optional: install the pinned Go toolchain
vfox exec golang -- go tool task build        # writes ./bin/librecode
./bin/librecode --help
```

If you do not use [vfox](https://vfox.lhan.me/), install a Go toolchain matching `go.mod`. Development tools such as [Task](https://taskfile.dev/) are declared in `go.mod` and invoked with `go tool`.

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
```

Send a one-shot prompt:

```bash
librecode prompt "summarize this repo"
librecode prompt --resume "continue from the last session"
```

Inspect available models and run a built-in tool directly:

```bash
librecode model list
librecode model list --all "claude"
librecode tool list
librecode tool run read '{"path":"README.md"}'
librecode tool run bash '{"command":"go test ./...","timeout":120}'
```

For scripts and benchmarks, `prompt` can write machine-readable run metrics and select direct or hybrid tool exposure:

```bash
librecode prompt --tool-strategy direct --metrics-json metrics.json "run the tests"
```

Manage sessions:

```bash
librecode session new "docs pass"
librecode session list
librecode session show <session-id>
```

## Slash commands

Type `/` in the composer to search the commands available in the current runtime. Common commands include:

| Command | Purpose |
| --- | --- |
| `/model`, `/scoped-models` | Select a model or configure the model set used by keyboard cycling. |
| `/login`, `/logout`, `/auth` | Manage provider credentials and inspect authentication state. |
| `/new`, `/resume`, `/name`, `/clone`, `/fork`, `/tree` | Create, resume, label, branch, and navigate durable sessions. |
| `/context`, `/compact` | Inspect context usage or summarize older history. |
| `/skill`, `/skill:<name>` | List or load an Agent Skill. |
| `/agents`, `/agents profiles` | Inspect subagent tasks or effective profiles, tool allowlists, and diagnostics. |
| `/tasks` | List, inspect, or cancel durable background tool executions. |
| `/workflows` | Inspect or cancel durable workflow runs. |
| `/settings`, `/hotkeys` | Change persisted session preferences or view keybindings. |
| `/reload` | Reload authentication and model runtime state. |

Unrecognized slash-prefixed text is sent to the model rather than silently discarded. Extension commands may add to this surface.

## Image attachments

PNG images can be pasted from the clipboard with `Ctrl+V`/`Cmd+V` when the selected model supports image input. A prompt accepts up to four images, 5 MiB each and 20 MiB total.

## Keyboard behavior

In the editable chat composer:

| Key | Idle | While an agent run is active |
| --- | --- | --- |
| `Enter` | Submit a prompt | Steer the active run |
| `Shift+Enter` | Insert a newline | Queue a follow-up |
| `Ctrl+J` | Insert a newline | Insert a newline |
| `Alt+Enter` | Queue a follow-up | Queue a follow-up |
| `Alt+Up` | Restore the newest queued follow-up | Restore the newest queued follow-up |

A steer joins the current run at the next safe model-turn boundary. It does not cancel or rewrite an in-flight provider response, and it waits for every tool in the already admitted tool batch to settle. A follow-up stays in the terminal queue and starts only after the active run settles. Canceling or failing a run restores accepted but unconsumed steering ahead of queued follow-ups; it does not restart canceled work.

Some legacy terminals report `Shift+Enter` as plain `Enter`. Use `Ctrl+J` for a portable newline and `Alt+Enter` for a portable follow-up. During compaction, authentication, and modal panels, the current state-specific input gating takes precedence.

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

OpenAI-compatible built-ins: `cerebras`, `deepseek`, `fireworks`, `groq`, `mistral`, `moonshotai`, `moonshotai-cn`, `opencode`, `opencode-go`, `openrouter`, `vercel-ai-gateway`, `xai`, and `zai`.

The default assistant config is:

```yaml
assistant:
  provider: openai-codex
  model: gpt-5.6-sol
  thinking_level: off
  retry:
    enabled: true
    max_attempts: 3
    base_delay: 2s
    max_delay: 30s
```

Built-in providers are limited to API families librecode implements directly: OpenAI/Codex Responses, OpenAI-compatible chat completions, and Anthropic Messages. At startup librecode can enrich those built-ins from the models.dev catalog so model pickers and context budgeting know current context windows, output limits, costs, reasoning support, and image support. Additional providers can still be added through custom model/provider definitions.

Credentials can come from:

- `/login openai-codex` for ChatGPT/Codex subscription OAuth;
- `/login anthropic-claude` for Claude Pro/Max OAuth;
- `/login <provider> <api-key>` for API-key providers such as `anthropic`, `openai`, `openrouter`, or `zai`;
- provider-specific environment variables: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `ANTHROPIC_OAUTH_TOKEN`, `AZURE_OPENAI_API_KEY`, `CEREBRAS_API_KEY`, `DEEPSEEK_API_KEY`, `FIREWORKS_API_KEY`, `GROQ_API_KEY`, `MISTRAL_API_KEY`, `OPENCODE_API_KEY`, `OPENROUTER_API_KEY`, `AI_GATEWAY_API_KEY`, `XAI_API_KEY`, and `ZHIPU_API_KEY`/`ZAI_API_KEY`;
- a normalized `<PROVIDER>_API_KEY` variable for custom provider IDs;
- custom provider/model definitions in the runtime model document.

Provider IDs are configured with `LIBRECODE_ASSISTANT_PROVIDER` or `assistant.provider`; model IDs use `LIBRECODE_ASSISTANT_MODEL` or `assistant.model`.

## Configuration

librecode resolves configuration in this order:

1. `LIBRECODE_*` environment variables
2. the YAML file selected by `--config <path>`, when supplied
3. otherwise `./.librecode/config.yaml`, then `~/.librecode/config.yaml` or `$LIBRECODE_HOME/config.yaml`
4. built-in defaults

`--config` selects a file; environment variables still override values from that file.

Useful commands:

```bash
librecode config show
librecode config validate
```

See [`config.example.yaml`](config.example.yaml) for a commented configuration template. Durable background execution is bounded by the `tasks` settings described there. The in-progress loader text defaults to `Shenaniganing...` and is configurable with `app.working_loader.text`.

Built-in memory limits protect untrusted input and remote bodies: prompt stdin and tool JSON stdin are capped at 1 MiB, provider response/error bodies at 16 MiB.

Default global persistence lives under one librecode home:

- librecode home: `~/.librecode` or `$LIBRECODE_HOME`
- config: `~/.librecode/config.yaml`
- sessions database: `~/.librecode/librecode.db`
- auth storage: `~/.librecode/auth.json`

Project-local overrides live under `./.librecode/` too. If `./.librecode/auth.json` or `./.librecode/librecode.db` exists, librecode uses it instead of the global file for that project.

## Instructions and Skills

librecode loads layered `AGENTS.md` instructions from the user home and from the workspace hierarchy down to the current directory. This lets a repository or subdirectory supply coding conventions without baking them into global configuration.

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
user-invocable: true
disable-model-invocation: false
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

Inside chat, `/skill` lists discovered skills. Use `/skill my-skill` or `/skill:my-skill` to load a skill through the read-tool path and render a `loaded skill my-skill` block. User-invocable skills also appear in slash autocomplete as `/skill:<name>`. `allowed-tools` is metadata today, not an enforced security boundary.

See [`docs/skills.md`](docs/skills.md) for validation and activation details.

## Subagents and workflows

> [!NOTE]
> Subagents, background tools, dynamic workflows, and low-level Lua UI overrides are advanced pre-release surfaces. Their contracts may change before 1.0.

Top-level sessions can delegate focused work to durable asynchronous subagents. librecode includes:

- `explore`, a read-only repository investigator;
- `general`, a coding agent whose mutating tools require an explicit `permissions: allow` profile.

Custom profiles are Markdown files in `./.librecode/agents/` or `$LIBRECODE_HOME/agents/`. Profiles can select tools, provider/model/thinking overrides, timeout, and mutation policy. Each task runs in an isolated child session, survives as durable state, and can be inspected or canceled from the terminal. Queued tasks resume after restart; interrupted task transcripts remain available.

For coordinated work, the model-visible `workflow` tool launches MVM Go programs against a small `librecode/workflow` API. Workflows can start, wait for, list, cancel, and pipeline subagents while the main chat remains responsive. Workflow runs and their child tasks are persisted in SQLite and visible through `/workflows`.

See [`docs/subagents.md`](docs/subagents.md) for profiles and lifecycle behavior.

## Extensions

Extensions are trusted local code that runs in the same process as librecode. Lua is the first supported runtime; the host is designed so additional runtimes can be added later. Because extensions have direct access to runtime state, only install ones you've reviewed or trust the source of.

Extensions are declared with `extensions.use` in config. The default source is:

```yaml
extensions:
  enabled: true
  use:
    - path:.librecode/extensions
```

Extension source declarations accept strings and object entries with versions:

```yaml
extensions:
  use:
    - official:vim-mode
    - github:example/librecode-extension
    - github:example/monorepo//extensions/fancy
    - path:.librecode/extensions/local-dev
    - source: github:example/librecode-extension
      version: v1.2.3
```

Startup loads only entries declared in `extensions.use`; extra directories on disk are ignored. The current CLI loads configured sources and can execute registered commands. Package-manager operations for installing or changing `official:` and `github:` sources remain roadmap work; use `path:` for local extensions today.

The default chat UI is Go-owned and extensions are optional customization. Use `--no-extensions` to disable configured extensions for a single run.

Current extension capabilities include:

- registering commands and extension-backed tools;
- listening to low-level runtime events;
- intercepting key input with priorities;
- reading and mutating runtime buffers such as `composer`, `status`, `transcript`, `thinking`, and `tools`;
- creating namespaces, autocmds, and keymaps through a Neovim-inspired Lua API.

For architecture, roadmap, and API details, see:

- [`docs/adr/0001-programmable-runtime.md`](docs/adr/0001-programmable-runtime.md)
- [`docs/runtime-architecture.md`](docs/runtime-architecture.md)
- [`docs/session-context.md`](docs/session-context.md)
- [`docs/extension-runtime.md`](docs/extension-runtime.md)
- [`docs/extension-manager.md`](docs/extension-manager.md)
- [`docs/extension-roadmap.md`](docs/extension-roadmap.md)
- [`docs/extension-api.md`](docs/extension-api.md)
- [`docs/rendering-boundary.md`](docs/rendering-boundary.md)
- [`docs/skills.md`](docs/skills.md)

Inspect extensions or run a registered extension command:

```bash
librecode extension list
librecode extension run <command> [args...]
```

## Tools and execution

The direct coding toolkit stays deliberately small. No external tool-server lifecycle is required.

| Tool | Mutates? | Purpose |
| --- | --- | --- |
| `read` | No | Read text or images with offsets and truncation controls. |
| `ls` | No | List directory entries. |
| `find` | No | Search paths with glob patterns. |
| `grep` | No | Search file contents with regex or literal matching. |
| `ast` | No | Inspect supported source syntax structurally with outlines, symbol trees, queries, and node views. |
| `fetch` | No | Fetch an explicit HTTP(S) URL as Markdown, text, or HTML. It does not perform web search. |
| `write` | **Yes** | Create or overwrite a file. |
| `edit` | **Yes** | Apply exact, unique text replacements. |
| `bash` | **Yes** | Execute shell commands with timeout and output limits. |

At the top level, librecode can augment these with extension tools, durable `agent_*` controls, asynchronous workflow launch, background envelopes for long-running tool calls, and the `execute` orchestration tool. `--tool-strategy direct` omits `execute`; the default `hybrid` strategy exposes both direct tools and code mode.

The `bash` tool executes commands directly in your shell with the permissions of the librecode process. There is no allowlist or interactive confirmation for the top-level assistant. Background subagents separately enforce their profile tool allowlist and permission policy.

Native Windows is not supported. Windows users should run librecode through WSL.

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
librecode model [list [search] [--all]]
librecode tool list
librecode tool run <name> [json-args|-] [--cwd path]
librecode extension list
librecode extension run <command> [args...]
librecode config show
librecode config validate
librecode migrate
librecode completion <bash|fish|powershell|zsh>
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
internal/agent/         Built-in and project/user subagent profile discovery
internal/agenttask/     Durable asynchronous subagent task execution
internal/assistant/     Prompt/session orchestration, lifecycle hooks, tool execution, persistence
internal/auth/          Provider credential storage, OAuth flows, and token refresh
internal/browser/       Cross-platform browser opener helpers
internal/compaction/    Pure compaction planning, summaries, and file-operation preservation
internal/config/        Viper config defaults, loading, and validation
internal/contextwindow/ Context budgets, token estimates, contributors, and usage-led estimates
internal/core/          Resources: system prompts, context files, skills, slash prompts
internal/database/      SQLite repositories and migrations
internal/di/            Service wiring with samber/do
internal/event/         Runtime event spine and stream helpers
internal/executeworker/  Isolated MVM execution worker/client boundary
internal/extension/     Extension host and Lua runtime API bridge
internal/llm/           Provider-neutral LLM DTOs, finish reasons, usage, and typed errors
internal/llmconv/       Shared conversions between model usage and LLM DTOs
internal/mapsutil/      Small map-clone helpers with explicit nil/empty semantics
internal/model/         Provider/model registry, catalog discovery, and auth resolution
internal/provider/      Provider HTTP/SSE wire clients and protocol normalization
internal/terminal/      Interactive terminal UI and terminal-specific subpackages
internal/tool/          Built-in coding tools
internal/tooltask/      Durable background tool execution
internal/transcript/    Shared transcript roles and tool-event formatting
internal/vinfo/         Version metadata injected at build time
internal/workflow/      Durable dynamic subagent workflow runtime
```

## Release

Releases are built by GoReleaser from `v*.*.*` tags:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release workflow cross-compiles Linux and macOS binaries, archives them, generates checksums, and publishes a GitHub release.

## License

MIT. See [`LICENSE.txt`](LICENSE.txt).

<a href="https://sonarcloud.io/summary/new_code?id=omarluq_librecode"><img src="https://sonarcloud.io/images/project_badges/sonarcloud-dark.svg" alt="SonarCloud Quality Gate"></a>
