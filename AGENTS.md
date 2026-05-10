# librecode - Agent Instructions

## Project overview

librecode is a local-first AI coding assistant and programmable terminal runtime written in Go. The default product experience is Go-owned for performance and polish; Lua extensions are optional escape hatches for commands, keymaps, hooks, tools, and small overlays.

## Required validation

After code changes, run all of:

```bash
mise exec -- go test ./...
mise exec -- task build
mise exec -- task ci
```

Report the results before committing. Use `mise exec --` for Task/Go tooling in this repo.

## Common commands

```bash
mise exec -- task build          # build ./bin/librecode
mise exec -- task run            # build and run
mise exec -- task test           # tests with race detector
mise exec -- task test-coverage  # coverage report
mise exec -- task lint           # golangci-lint
mise exec -- task fmt            # auto-format and auto-fix lint issues
mise exec -- task fmt-check      # check formatting without modifying files
mise exec -- task ci             # non-mutating full CI pipeline
```

## Project structure

```text
cmd/librecode/        # Cobra CLI commands and entrypoint
internal/assistant/   # assistant runtime, model retry, tool loop, slash commands
internal/auth/        # provider auth and credential storage
internal/config/      # YAML/env/default config loading and validation
internal/core/        # resources, skills, system prompt helpers
internal/database/    # SQLite sessions/migrations and ksqlDB client
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

## Windows notes

The public `bash` tool requires a Bash-compatible shell. On Windows, librecode should prefer configured/Git Bash/MSYS2/Cygwin/WSL Bash and should not silently fall back to `cmd.exe` for Bash semantics.
