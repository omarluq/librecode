# Extension runtime architecture

## Purpose

This document describes the current extension runtime and the target architecture librecode is moving toward.

The short version:

- today, extensions can already register commands, tools, keymaps, namespaces, autocmd-like handlers, and runtime buffer mutations
- tomorrow, those primitives should become the main architecture of the terminal/runtime itself

This document is intentionally architecture-first. See `docs/extension-api.md` for the current user-facing Lua API and `docs/rendering-boundary.md` for the control-plane/rendering-kernel split.

## Design goals

The extension system is designed around a few principles.

### 1. Low-level primitives over special cases

We prefer general mechanisms over one-off feature hooks.

Good examples:

- buffers
- windows
- layout
- UI drawing
- events
- keymaps
- commands
- namespaces

Less desirable long-term examples:

- feature-specific hardcoded plugin points such as a dedicated Vim composer API
- product-specific host APIs such as `transcript.append` or `thinking.show`

The architectural rule is: if an API name is a product noun, it probably belongs in Lua as a helper module. If it is a primitive, it belongs in the Go runtime kernel.

### 2. Trusted local code

Extensions are trusted local Lua code.

librecode follows a Unix-style trust model:

- extensions may read, write, shell out, and otherwise footgun if the user installs such code
- the runtime should not pretend to sandbox them
- the runtime should still defend its own invariants: no deadlocks, corrupted state, or silent event-loop breakage

### 3. Go-owned default UI with optional Lua customization

The terminal chat UI should remain polished and fast even when no Lua extensions are loaded. Lua is an optional customization layer, not a requirement for the core experience.

Go owns the default product UX and hot rendering backend: measuring, wrapping, clipping, batching, viewporting, style application, and mature transcript/composer/status rendering. Lua can still opt into overriding or augmenting these surfaces through public primitives.

That means users should be able to:

- rewrite the composer experience
- replace transcript rendering
- add or remove panels
- intercept prompt submission
- replace assistant orchestration
- build an interface that looks nothing like the stock chat layout

## Current architecture

## Loader

Extensions are loaded by `internal/extension.Manager` from configured `extensions.use` entries.

Configured defaults currently come from `config/loader.go`:

```yaml
extensions:
  enabled: true
  use:
    - path:.librecode/extensions
```

Source schemes:

- `path:<path>` loads extension files or directories from disk today.
- `official:<name>` names first-party extensions for the extension manager.
- `github:<owner>/<repo>` and `github:<owner>/<repo>//<subdir>` reserve the remote install/update interface.

No bundled extension root is prepended automatically. Passing `--no-extensions` disables configured Lua extensions for the current command without changing config.

The loader:

- resolves configured sources only
- resolves `path:` sources directly today
- resolves future `official:` and `github:` entries through the extension manager install store and lockfile
- discovers Lua files and directory manifests
- creates a dedicated Lua state per extension entry
- opens trusted standard libraries
- installs the `librecode` Lua module/API table
- executes the entry file
- records registered commands, tools, keymaps, and handlers

## Runtime model

Each loaded file has its own isolated Lua state, represented internally as a `luaExtension`.

The manager owns shared registries for:

- commands
- tools
- event handlers
- keymaps
- namespaces
- extension metadata

Current extension-visible state is event-oriented.

For terminal runtime events, Go creates a `TerminalEvent` with:

- `name`
- `key`
- `context`
- `buffers`
- `windows`
- `layout`

That event is copied into a mutable host-side structure (`luaHostEvent`) before Lua handlers run.

Handlers can then:

- mutate buffers
- append to buffers
- delete buffers
- mutate windows/layout
- enqueue low-level window-relative draw operations
- mark the event consumed
- stop later handlers

After handler execution, the accumulated result is applied back to the terminal app.

## Current exposed buffers

The terminal currently exposes these named buffers to extension handlers:

- `composer`
- `status`
- `transcript`
- `thinking`
- `tools`
- extension-created runtime buffers

It also exposes a window/layout model for active terminal events, including a `composer` window bound to the composer buffer. Extensions can now discover visible UI regions, mutate windows/layout, and enqueue low-level draw operations.

Important detail: these are not yet a complete unified buffer architecture for the entire application.

Today:

- `composer` is backed by the canonical composer buffer
- `status` exposes footer/status metadata and can be overlaid or overridden as a runtime buffer
- `transcript` exposes message/streaming counts plus bounded recent blocks; overriding it lets extensions replace the stock transcript text render
- `thinking` exposes thinking counts as metadata and can be overridden by extensions
- `tools` exposes tool-result counts as metadata and can be overridden by extensions
- custom buffers persist in `app.extensionRuntimeBuffers`

This is a good start, but not the final architecture.

## Current event surface

The terminal currently emits low-level extension events for:

- `startup`
- `key`
- `prompt_submit`
- `prompt_user_entry`
- `prompt_done`
- `model_delta`
- `thinking_delta`
- `tool_start`
- `tool_end`
- `resize`
- `render`

The assistant runtime also emits named extension lifecycle events through `Manager.Emit`, currently including:

- `before_agent_start`
- `agent_end`

This is enough for UI/runtime observation, but not enough for full assistant-loop replacement.

## Current strengths

`librecode extension list` doubles as a lightweight diagnostics surface: it shows registered commands, tools, keymaps, handlers, active timers, and cumulative Lua execution duration per loaded file.

The current system already proves a few important things:

- extensions can own focused UX behavior, such as custom composer modes or small overlays
- key handling can be intercepted and prioritized
- buffer mutation can drive visible terminal behavior
- one extension file can expose commands, tools, and event handlers together
- Lua can be treated as a real runtime integration layer, not just a config format

## Current limitations

### 1. Buffers are not yet the universal internal model

Core UI state is increasingly exposed as buffers, but much of it is still projected from Go-owned structures.

Current stock runtime buffers include `composer`, `status`, `transcript`, `thinking`, and `tools`. The composer is canonical; `transcript`, `thinking`, and `tools` expose lightweight metadata buffers. The `transcript` buffer also exposes a bounded `blocks` snapshot for recent message/streaming data.

Transcript read/write convenience should stay out of the Go host API. Use generic `buf` operations directly or implement product helpers as Lua modules on top.

### 2. Render/layout is still host-first

Extensions can mutate the active layout, enqueue low-level window-relative draw operations during render events, and mark a window with `renderer = "extension"` to take renderer ownership.

When an extension owns a window, the stock Go renderer skips that window and only extension draw operations/cursor placement are applied. This is useful for opt-in custom windows and focused experiments. The default Go renderer still owns the stock chat drawing order, composer, status, autocomplete, and transcript rendering.

That is intentional. Transcript rendering and the core chat UI are hot, complex surfaces and should stay Go-owned unless an opt-in extension can match visual parity and performance through generic primitives.

### 3. Event surface still needs more lifecycle points

The runtime exposes the core terminal and streaming lifecycle now:

- startup
- key
- prompt_submit
- prompt_user_entry
- prompt_done
- model_delta
- thinking_delta
- tool_start
- tool_end
- resize
- render

The next missing event families are deeper runtime replacement hooks:

- shutdown
- tick
- session_load
- session_save
- prompt_prepare
- model_request
- tool_delta
- message_append
- transcript_render

### 4. Jobs and scheduling are still incomplete

A programmable runtime needs async primitives so extensions can do useful work without blocking the core loop. Timer primitives now exist (`timer.defer`, `timer.interval`, `timer.stop`), but process/job spawning and a general scheduler are still missing.

### 5. The default assistant/runtime loop is still mostly owned by Go

Extensions can hook around the edges, but they cannot yet cleanly replace the whole loop.

## Target architecture

The target state is a more genuinely programmable runtime.

## 1. Events become first-class runtime plumbing

The runtime should expose a richer event bus with:

- event names
- structured payloads
- priorities
- consumption/stopping semantics
- consistent ordering guarantees

Extensions should be able to observe and rewrite default behavior by intercepting these events.

## 2. Buffers become the primary mutable UI model

The system should move beyond three special terminal buffers and support a richer model:

- named runtime buffers
- write-side transcript/message blocks
- scratch buffers
- UI-owned buffers
- metadata and annotations per buffer

Longer term, the architecture should support concepts similar to extmarks/highlights/namespaces.

## 3. Keymaps and commands become standard routing layers

Key handling should increasingly go through generic keymap dispatch rather than bespoke feature logic.

Similarly, user-visible commands should be registered and dispatched through the same public extension machinery used by bundled features.

## 4. Layout/render becomes programmable

To fully reskin librecode, extensions need a way to:

- define visible regions or windows
- bind buffers to regions
- render text and metadata
- control footer/status/cursor placement
- replace the stock terminal layout entirely

This can start simple, but it must eventually exist.

## 5. Assistant/runtime flow becomes replaceable

The long-term system should allow extensions to:

- rewrite prompts before submission
- replace the default request/response loop
- alter how model deltas become transcript blocks
- control how tool activity is represented
- drive non-chat workflows entirely

## Go core vs Lua extension layer

The intended split is:

- **Go kernel**: terminal I/O, event dispatch, Lua VM management, buffers, windows, layout, UI draw backend, measuring, wrapping, clipping, batching, viewporting, keymaps, commands, jobs/timers, model/tool/session/config primitives, and invariant protection.
- **Lua extension layer**: optional keymaps, commands, hooks, small overlays, custom windows, prompt/context tweaks, experimental composer modes, reskins, and alternate workflows.

Lua can own a window renderer, but complex renderers should use Go-backed generic primitives rather than ad hoc Lua string math in hot paths.

Optional Lua behavior should use the same public API available to users; the default Go UI should not depend on bundled Lua to feel complete.

See `docs/runtime-architecture.md` for the full responsibility boundary and `docs/extension-roadmap.md` for the migration plan.

## Migration strategy

This should be incremental, not a rewrite.

### Phase 1: establish primitives

Already in progress:

- Lua module loading with `require("librecode")`
- commands
- tools
- keymaps
- namespaces
- autocmd-like handlers
- event consumption/stopping
- runtime buffer operations

### Phase 2: broaden runtime events

Add more event sources across terminal and assistant code paths.

### Phase 3: centralize buffer ownership

Move more terminal-visible state behind shared buffer-like abstractions.

### Phase 4: expose render/layout

In progress:

- render and resize events
- layout get/set
- window create/set/delete
- low-level UI draw/cursor operations
- per-window renderer ownership via `renderer = "extension"`

Next: rebuild more of the stock UI against those same public primitives.

### Phase 5: expose assistant/runtime replacement hooks

Let extensions own more of the request/model/tool/session loop.

### Phase 6: move product convenience into Lua modules

Optional only. Avoid expanding Go with product-specific extension APIs. User or project helper modules can wrap primitives, but librecode no longer ships auto-loaded Lua product modules.

## Documentation and planning split

Stable architecture docs live in `docs/`.

Messy planning and working notes should live under the gitignored workspace:

- `.librecode/work/plans/`
- `.librecode/work/research/`
- `.librecode/work/sketches/`

Promote only stable decisions into tracked docs.
