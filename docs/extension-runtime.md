# Extension runtime architecture

## Purpose

This document describes the current extension runtime and the target architecture librecode is moving toward.

The short version:

- today, extensions can already register commands, tools, keymaps, namespaces, autocmd-like handlers, and runtime buffer mutations
- tomorrow, those primitives should become the main architecture of the terminal/runtime itself

This document is intentionally architecture-first. See `docs/extension-api.md` for the current user-facing Lua API.

## Design goals

The extension system is designed around a few principles.

### 1. Low-level primitives over special cases

We prefer general mechanisms over one-off feature hooks.

Good examples:

- buffers
- events
- keymaps
- commands
- namespaces

Less desirable long-term examples:

- feature-specific hardcoded plugin points such as a dedicated Vim composer API

### 2. Trusted local code

Extensions are trusted local Lua code.

librecode follows a Unix-style trust model:

- extensions may read, write, shell out, and otherwise footgun if the user installs such code
- the runtime should not pretend to sandbox them
- the runtime should still defend its own invariants: no deadlocks, corrupted state, or silent event-loop breakage

### 3. Default UI as just another implementation

The bundled terminal chat UI should increasingly become a client of the extension/runtime primitives, not a privileged special case.

That means users should eventually be able to:

- rewrite the composer experience
- replace transcript rendering
- add or remove panels
- intercept prompt submission
- replace assistant orchestration
- build an interface that looks nothing like the stock chat layout

## Current architecture

## Loader

Extensions are loaded by `internal/extension.Manager`.

Default roots today:

1. `extensions/`
2. configured `extensions.paths` entries

Configured defaults currently come from `config/loader.go`:

- `extensions`
- `.librecode/extensions`

The official bundled root is always deduped in front by `internal/extension.DefaultLoadPaths`.

The loader:

- resolves each configured file or directory
- recursively discovers `*.lua`
- creates a dedicated Lua state per file
- opens trusted standard libraries
- installs the `librecode` Lua module/API table
- executes the file
- records registered commands, tools, keymaps, modes, and handlers

## Runtime model

Each loaded file has its own isolated Lua state, represented internally as a `luaExtension`.

The manager owns shared registries for:

- commands
- tools
- composer modes
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

That event is copied into a mutable host-side structure (`luaHostEvent`) before Lua handlers run.

Handlers can then:

- mutate buffers
- append to buffers
- delete buffers
- mark the event consumed
- stop later handlers

After handler execution, the accumulated result is applied back to the terminal app.

## Current exposed buffers

The terminal currently exposes these named buffers to extension handlers:

- `composer`
- `status`
- `transcript`
- extension-created runtime buffers

Important detail: these are not yet a complete unified buffer architecture for the entire application.

Today:

- `composer` is backed by the terminal editor state
- `status` is backed by the footer/status string
- `transcript` is mostly a façade for append/reset-style interactions
- custom buffers persist in `app.extensionRuntimeBuffers`

This is a good start, but not the final architecture.

## Current event surface

The terminal currently emits low-level extension events for:

- `key`
- `prompt_submit`

The assistant runtime also emits named extension lifecycle events through `Manager.Emit`, currently including:

- `before_agent_start`
- `agent_end`

This is still too small for the intended design.

## Current strengths

The current system already proves a few important things:

- extensions can own substantial UX behavior, such as the bundled Vim composer mode
- key handling can be intercepted and prioritized
- buffer mutation can drive visible terminal behavior
- one extension file can expose commands, tools, and event handlers together
- Lua can be treated as a real runtime integration layer, not just a config format

## Current limitations

### 1. Buffers are not yet the universal internal model

Core UI state is still primarily owned by Go structs, with extension buffers layered on top.

We need to move toward a world where more of the runtime is expressed as first-class named buffers and buffer-like objects.

### 2. Render/layout is not replaceable yet

Extensions cannot yet fully control layout or paint the screen.

They can influence composer/status/transcript behavior, but they cannot truly reskin the application from first principles.

### 3. Event surface is too small

The runtime needs more observable and interceptable lifecycle points.

Examples:

- startup
- shutdown
- resize
- render
- tick
- session_load
- session_save
- prompt_prepare
- prompt_submit
- model_request
- model_delta
- thinking_delta
- tool_start
- tool_delta
- tool_end
- message_append
- transcript_render

### 4. Jobs, timers, and scheduling are missing

A programmable runtime needs async primitives so extensions can do useful work without blocking the core loop.

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
- transcript/message buffers
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

Add enough rendering primitives that the stock UI can be incrementally rebuilt using the public API.

### Phase 5: expose assistant/runtime replacement hooks

Let extensions own more of the request/model/tool/session loop.

## Documentation and planning split

Stable architecture docs live in `docs/`.

Messy planning and working notes should live under the gitignored workspace:

- `.librecode/work/plans/`
- `.librecode/work/research/`
- `.librecode/work/sketches/`

Promote only stable decisions into tracked docs.
