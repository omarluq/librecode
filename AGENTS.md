# librecode - Agent Instructions

## Project overview

librecode is a local-first AI coding assistant and programmable terminal runtime written in Go. The default product experience is Go-owned for performance and polish; Lua extensions are optional escape hatches for commands, keymaps, hooks, tools, and small overlays.

## Required validation

After code changes, run all of:

```bash
vfox exec golang -- go test ./...
vfox exec golang -- go tool task build
vfox exec golang -- go tool task ci
```

Report the results before committing. Use `vfox exec golang --` for Go tooling and invoke development tools declared in `go.mod` with `go tool`.

## Common commands

```bash
vfox exec golang -- go tool task build          # build ./bin/librecode
vfox exec golang -- go tool task run            # build and run
vfox exec golang -- go tool task test           # tests with race detector
vfox exec golang -- go tool task test-coverage  # coverage report
vfox exec golang -- go tool task lint           # golangci-lint
vfox exec golang -- go tool task fmt            # auto-format and auto-fix lint issues
vfox exec golang -- go tool task fmt-check      # check formatting without modifying files
vfox exec golang -- go tool task ci             # non-mutating full CI pipeline
```

## Project structure

```text
cmd/librecode/        # Cobra CLI commands and entrypoint
internal/assistant/   # assistant runtime, model retry, tool loop, slash commands
internal/auth/        # provider auth and credential storage
internal/config/      # YAML/env/default config loading and validation
internal/core/        # resources, skills, system prompt helpers
internal/database/    # SQLite sessions/migrations and repositories
internal/di/          # samber/do dependency registration
internal/event/       # in-process event bus
internal/extension/   # trusted Lua extension host/runtime
internal/model/       # model/provider registry
internal/terminal/    # TUI rendering/input/session UX
internal/tool/        # built-in tools: read, bash, edit, write, grep, find, ls
internal/vinfo/       # build version info
```

## Architecture direction

- Keep the default terminal UI, transcript rendering, composer, autocomplete, resize behavior, sessions, tools, and provider orchestration in Go.
- Keep Lua extensions optional and trusted. They may customize behavior, but the default UX must remain fast and polished without extensions.
- Prefer primitive extension APIs (`buf`, `win`, `layout`, `ui`, `keymap`, `timer`, lifecycle events) plus higher-level helpers in Lua/userland.
- Avoid adding product-specific host APIs unless they are clearly needed by the Go core.
- Skills follow the Agent Skills `SKILL.md` directory convention with project-local roots taking priority.

Useful docs:

- `docs/runtime-architecture.md`
- `docs/extension-api.md`
- `docs/extension-runtime.md`
- `docs/extension-roadmap.md`
- `docs/skills.md`
- `docs/rendering-boundary.md`

## Engineering principles

- Preserve documented public behavior, persisted data, and extension contracts unless the task explicitly authorizes a breaking change. When a breaking change is authorized, remove obsolete paths instead of adding indefinite compatibility layers, fallbacks, or migrations.
- Choose the simplest implementation that fully satisfies the current requirements. Avoid speculative abstractions, configuration, and indirection.
- Grow the system in working end-to-end layers. Start with the smallest complete version, then add capabilities without replacing working behavior with unfinished complexity.
- Keep components modular and concerns clearly separated.
- Prefer established, well-maintained libraries when they reduce overall complexity or improve reliability. Do not reimplement common functionality without a clear reason.
- Inspect existing code, dependencies, documentation, and types before writing an implementation or adding a package. Prefer the standard library and existing project dependencies when they meet the requirements.
- Make architectural decisions for the long term. Avoid knowingly temporary production implementations intended to be replaced later unless the task explicitly requires a documented incremental step.

## Code style

- Follow existing package patterns and keep changes small/focused.
- Use `oops.In("domain").Code("code").Wrapf(err, "message")` for contextual errors where the package already uses `samber/oops`.
- Never ignore errors; `errcheck` with `check-blank: true` is enabled.
- Handle `fmt.Fprintf`/`fmt.Fprintln` return values.
- Keep the default render path hot and allocation-conscious; do not route default UI through Lua unless explicitly required and benchmarked.
- Prefer table-driven tests for core behavior and regression tests for terminal rendering bugs.

## When adding CLI commands

1. Add a focused file under `cmd/librecode/`.
2. Expose a `newXCmd()` constructor returning `*cobra.Command`.
3. Register it from the appropriate parent command.
4. Add tests for argument validation and user-visible behavior when practical.

## When adding services

1. Create the service under the appropriate `internal/` package.
2. Register dependencies in `internal/di/register.go` when the service is app-wide.
3. Inject via `do.MustInvoke`/`do.Invoke` following existing patterns.
