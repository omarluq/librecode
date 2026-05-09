# Runtime architecture direction

## Status

This document captures the architectural direction for librecode after the initial extension-runtime work.

It is intentionally opinionated. The purpose is to keep future work aligned around a small runtime kernel instead of accidentally rebuilding a fixed chat application through a growing set of product-specific APIs.

Related docs:

- [`docs/adr/0001-programmable-runtime.md`](adr/0001-programmable-runtime.md)
- [`docs/extension-runtime.md`](extension-runtime.md)
- [`docs/extension-api.md`](extension-api.md)
- [`docs/rendering-boundary.md`](rendering-boundary.md)

## North star

librecode should become a programmable terminal runtime.

The default AI chat UI is the bundled product built on top of that runtime, not the core identity of the runtime itself.

A useful shorthand:

> Go is the product core and runtime kernel. Lua is the optional extension layer.

Go provides the polished default chat experience plus sharp primitives for user customization. Lua composes those primitives for keymaps, commands, hooks, small overlays, prompt/context tweaks, and custom workflows.

Important rendering nuance: Lua is useful for control and customization, but Go remains the default UI implementation and fast rendering backend. Complex hot renderers should stay Go-owned unless an opt-in extension can match visual parity and performance through public primitives.

## Responsibility boundary

### Go owns runtime infrastructure

Go should own the pieces that require native integration, performance, persistence, process control, or terminal access:

- terminal input/output and screen flushing
- process lifecycle and signal handling
- extension loading and Lua VM management
- event dispatch and transaction application
- buffers, windows, layout, and low-level UI drawing backends
- measuring, wrapping, clipping, style application, and draw batching
- viewport and virtual-list primitives for large histories
- keymap/command/autocmd registration and dispatch
- jobs, timers, and scheduling primitives
- model/provider clients as callable primitives
- tool execution as callable primitives
- session/database/auth/config stores as callable primitives
- guardrails for core invariants: no deadlocks, panics, corrupted state, or runaway unbounded projections

Go should expose mechanisms for extensions while keeping the default product behavior polished and self-contained.

### Lua owns optional customization

Lua should own behavior users choose to add or override:

- custom keymaps and commands
- small overlays and custom windows
- focused composer modes or editor experiments
- prompt/context tweaks
- optional status/footer overlays
- custom tools and workflow hooks
- reskins, alternate UIs, custom workflows, and non-chat applications

The default chat UI should not require Lua extensions. It must remain fast and polished with extensions disabled.

## Primitive API rule

Core host APIs should be named after primitives, not product nouns.

Prefer host APIs like:

- `buf.*`
- `win.*`
- `layout.*`
- `ui.*`
- `event.*`
- `keymap.*`
- `command.*`
- `job.*`
- `timer.*`
- `model.*`
- `tool.*`
- `store.*`

Avoid new host APIs like:

- `transcript.append`
- `composer.submit_mode`
- `thinking.show`
- `chat.add_message`
- `vim.register_mode`

Product-level convenience helpers can still exist in user/project Lua modules, not the Go kernel:

```lua
local chat = require("my_workflow.chat")
chat.append_message("assistant", "hello")
```

That helper should compose primitive APIs internally, for example by finding a window with role `transcript`, resolving its buffer, and appending a structured block through generic buffer operations.

## Current code assessment

### Keep

These parts fit the direction and should be strengthened:

- trusted local Lua extension loading with one Lua state per file
- open standard libraries for the Unix-style footgun model
- event handlers with priorities, consume, and stop semantics
- generic keymap targets by buffer, window, role, or global scope
- the buffer API as the main mutable state surface
- window discovery and mutation APIs
- layout get/set APIs
- low-level UI draw/cursor operations
- per-window renderer ownership via `renderer = "extension"`
- canonical composer buffer state
- bounded transcript snapshots to prevent render-loop stalls
- docs/ADR split plus gitignored `.librecode/work/` for messy planning

### Keep temporarily, then migrate

These pieces are useful today but should not define the long-term architecture:

| Area | Current value | Migration direction |
| --- | --- | --- |
| Go stock renderer | Provides the default product UI and high-quality hot rendering. | Keep it as the default; allow optional Lua overrides only when explicitly enabled. |
| Bounded transcript `buffer.blocks` snapshot | Practical extension read-side access without unbounded projections. | Keep bounded and treat as generic structured buffer data, not a separate transcript host API. |
| Runtime buffer names like `composer`, `transcript`, `status` | Useful default buffers and roles. | Treat them as conventional defaults, not privileged API concepts. |
| `action.run("...")` | Simple bridge for host actions. | Move toward generic commands/events and lower-level primitives; keep only kernel actions in Go. |
| Default layout generation in Go | Gives the app a stable default layout. | Keep as the default; expose layout primitives for optional overrides. |

### Nuke or avoid

These patterns fight the target architecture:

- new product-specific Go APIs for transcript/composer/thinking/tool presentation
- special Vim/composer mode registration hooks
- unbounded transcript/text projections during render or streaming events
- hidden extension behavior that bypasses event transactions
- APIs that mutate application state outside an active event/scheduled transaction
- treating `transcript`, `composer`, or `status` as more than default buffers/windows/roles

The recent idea of `librecode.transcript.append()` and `librecode.transcript.clear()` is intentionally rejected as a core API direction. The desired replacement is generic buffer/object mutation plus optional Lua helper modules.

## What currently works

The current runtime can already support meaningful customization:

- extensions can intercept keys with priorities
- extensions can mutate the composer buffer
- extensions can find the composer window by role and get its bound buffer
- extensions can own a window renderer and draw directly into it
- extensions can implement optional composer modes and render overrides in Lua
- render and resize events exist
- layout/window mutations apply back to the terminal runtime
- transcript/thinking/tools/status are exposed as lightweight buffers or metadata surfaces

This is enough to prove the model, but not enough to make the entire app replaceable yet.

## What still does not work

The remaining architectural gaps are mostly about ownership:

- default transcript/tool rendering is Go-owned by design
- transcript is not a true structured buffer yet; it is a bounded snapshot plus metadata buffer
- layout is not fully canonical; Go still derives the default stock layout
- the assistant prompt/model/tool loop is still primarily Go-owned
- jobs/processes and a general scheduler are missing
- extension renderers can draw, but there is no full highlight/extmark/namespace model yet
- generic rendering primitives are still too small for transcript-quality parity
- session/model/tool stores are not exposed as generic runtime primitives
- the default chat UI is Go-owned; Lua extensions are optional

## Target runtime model

### Buffers

Buffers are mutable state containers. They can hold plain text now and should evolve toward structured block/object buffers.

Examples:

- composer text
- transcript/message blocks
- thinking blocks
- tool result blocks
- status text
- scratch extension buffers

Buffers should exist independently of whether they are visible.

### Windows

Windows are views onto buffers.

A window owns view-specific data:

- buffer binding
- role
- position and dimensions
- viewport/scroll
- cursor position
- renderer owner
- metadata

Multiple windows should be able to show the same buffer differently.

### Layout

Layout arranges windows on the screen.

The long-term default chat layout should be regular layout state, not a hardcoded renderer assumption.

### UI drawing

`ui.*` is the low-level grid drawing layer for extensions that own a window renderer.

Go should back the hot/terminal-correct pieces of rendering. Lua should compose them.

Current first-pass primitives include terminal-width measurement, truncation, padding, wrapping, drawing text/lines/spans/boxes, drawing batches, clearing windows/regions, setting cursors, theme-token discovery, viewports, and virtual-list helpers for large histories.

Longer term this should still gain:

- namespace-scoped highlights
- extmarks/virtual text
- richer window viewport/scroll APIs
- renderer registration helpers

### Events

Events are the control plane.

Extensions should be able to observe, mutate, consume, stop, and emit events. Default behavior should increasingly be implemented as event handlers plus primitive mutations.

### Jobs and timers

A programmable runtime needs non-blocking work primitives:

- spawn jobs
- schedule callbacks
- debounce/throttle
- timers and intervals

Timers are now partially implemented (`timer.defer`, `timer.interval`, `timer.stop`). Job/process spawning and a general scheduler still need to be added before deeper Lua-owned runtime orchestration.

## Migration plan

### Phase 0: stop growing product host APIs

Before adding more APIs, classify them:

- primitive/kernel API: OK in Go
- product convenience API: implement in Lua
- compatibility API: document as temporary and do not expand

### Phase 1: generic structured buffers

Status: implemented for the current runtime surface.

Generic buffer operations now cover text, structured blocks, metadata variables, and clearing buffers. Transcript data is exposed as bounded `blocks` on the regular `transcript` buffer instead of through a transcript-specific Go API.

Shape:

```lua
lc.buf.append("transcript", {
  kind = "message",
  role = "assistant",
  text = "hello",
  metadata = {},
})

lc.buf.clear("transcript")
lc.buf.get_blocks("transcript", start, stop)
lc.buf.set_blocks("transcript", start, stop, blocks)
lc.buf.delete_blocks("transcript", start, stop)
lc.buf.get_var("transcript", "snapshot_count")
lc.buf.set_var("transcript", "owner", "my-extension")
```

This keeps transcript behavior generic while still supporting chat-like data.

### Phase 2: Lua helper modules

Optional user/project Lua modules may wrap primitives:

- workflow-specific helper modules under a project extension root
- optional chat/composer/status helpers maintained outside the Go host API

These modules should compose primitives and be replaceable.

### Phase 3: optional UI overrides, where primitives are ready

Keep default layout/status/composer behavior in Go. Allow user extensions to override specific windows or add overlays where parity is achievable.

Do not force-migrate complex hot renderers such as transcript rendering. Go keeps mature stock rendering for quality and performance while Lua remains an optional control/customization layer.

### Phase 4: runtime replacement hooks

Expose model/tool/session primitives and lifecycle events so Lua can own more of the assistant flow:

- prompt preparation
- model request creation
- model delta handling
- tool activity mapping
- session message persistence policy

### Phase 5: bare runtime mode

Eventually support a bare mode that loads only the kernel and user-selected extensions, without the stock chat distribution.

## Review checklist for future extension work

Before adding an API, ask:

1. Is this a primitive or a product noun?
2. Could this be implemented in Lua using existing primitives?
3. Does it require native Go integration, persistence, or terminal/process access?
4. Does it preserve bounded work on render/stream events?
5. Does it mutate through an event/scheduled transaction?
6. Is it useful for applications other than chat?
7. If it is compatibility-only, is that clearly documented?

If the answer points to optional customization, build it as a user/project Lua extension. If it is core default UX, keep it in Go.
