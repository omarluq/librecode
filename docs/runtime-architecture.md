# Runtime architecture direction

## Status

This document captures the architectural direction for librecode after the initial extension-runtime work.

It is intentionally opinionated. The purpose is to keep future work aligned around a small runtime kernel instead of accidentally rebuilding a fixed chat application through a growing set of product-specific APIs.

Related docs:

- [`docs/adr/0001-programmable-runtime.md`](adr/0001-programmable-runtime.md)
- [`docs/extension-runtime.md`](extension-runtime.md)
- [`docs/extension-api.md`](extension-api.md)

## North star

librecode should become a programmable terminal runtime.

The default AI chat UI is the bundled product built on top of that runtime, not the core identity of the runtime itself.

A useful shorthand:

> Go is the kernel. Lua is the product layer.

The kernel provides sharp primitives. Bundled Lua composes those primitives into chat, Vim mode, transcript rendering, statuslines, prompt history, and eventually assistant orchestration.

## Responsibility boundary

### Go owns runtime infrastructure

Go should own the pieces that require native integration, performance, persistence, process control, or terminal access:

- terminal input/output and screen flushing
- process lifecycle and signal handling
- extension loading and Lua VM management
- event dispatch and transaction application
- buffers, windows, layout, and low-level UI drawing backends
- keymap/command/autocmd registration and dispatch
- jobs, timers, and scheduling primitives
- model/provider clients as callable primitives
- tool execution as callable primitives
- session/database/auth/config stores as callable primitives
- guardrails for core invariants: no deadlocks, panics, corrupted state, or runaway unbounded projections

Go should expose mechanisms, not product policy.

### Lua owns product behavior

Lua should own the behavior that makes librecode feel like a particular application:

- default chat layout
- composer behavior
- Vim mode
- transcript presentation
- statusline/footer content
- prompt history UX
- autocomplete presentation
- skills/context assembly policy
- assistant loop policy
- model/tool activity presentation
- reskins, alternate UIs, custom workflows, and non-chat applications

Official bundled features should use the same public runtime API as user extensions wherever practical.

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

Product-level convenience helpers can still exist, but they should live in Lua modules, not the Go kernel:

```lua
local chat = require("librecode.chat")
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
- bundled Vim mode implemented in Lua through runtime primitives
- bounded transcript snapshots to prevent render-loop stalls
- docs/ADR split plus gitignored `.librecode/work/` for messy planning

### Keep temporarily, then migrate

These pieces are useful today but should not define the long-term architecture:

| Area | Current value | Migration direction |
| --- | --- | --- |
| Go stock renderer | Provides a working default UI while primitives mature. | Rebuild default UI as bundled Lua modules over buffer/window/layout/ui APIs. |
| Bounded transcript `buffer.blocks` snapshot | Practical extension read-side access without unbounded projections. | Keep bounded and treat as generic structured buffer data, not a separate transcript host API. |
| Runtime buffer names like `composer`, `transcript`, `status` | Useful default buffers and roles. | Treat them as conventional defaults, not privileged API concepts. |
| `action.run("...")` | Simple bridge for host actions. | Move toward generic commands/events and lower-level primitives; keep only kernel actions in Go. |
| Default layout generation in Go | Gives the app a stable fallback layout. | Make layout state canonical and move default layout policy into Lua. |

### Nuke or avoid

These patterns fight the target architecture:

- new product-specific Go APIs for transcript/composer/thinking/tool presentation
- special Vim/composer mode registration hooks
- unbounded transcript/text projections during render or streaming events
- direct Go ownership of UI policy that could be expressed through public primitives
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
- Vim mode owns composer behavior and rendering from Lua
- render and resize events exist
- layout/window mutations apply back to the terminal runtime
- transcript/thinking/tools/status are exposed as lightweight buffers or metadata surfaces

This is enough to prove the model, but not enough to make the entire app replaceable yet.

## What still does not work

The remaining architectural gaps are mostly about ownership:

- default transcript/status/tool rendering is still mostly Go-owned
- transcript is not a true structured buffer yet; it is a bounded snapshot plus metadata buffer
- layout is not fully canonical; Go still derives the default stock layout
- the assistant prompt/model/tool loop is still primarily Go-owned
- jobs, timers, and scheduling are missing
- extension renderers can draw, but there is no full highlight/extmark/namespace model yet
- session/model/tool stores are not exposed as generic runtime primitives
- the default chat UI has not been moved into bundled Lua modules

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

Longer term this should gain:

- highlights
- namespaces
- extmarks/virtual text
- clipping helpers
- draw-lines and region operations

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

Move product convenience into bundled Lua modules:

- `librecode.chat`
- `librecode.composer`
- `librecode.statusline`
- `librecode.transcript` as a Lua helper module, not a Go host API

These modules should compose primitives and be replaceable.

### Phase 3: default UI in Lua

Move default layout/status/composer/transcript rendering into official Lua extensions under `extensions/`.

Go keeps a minimal fallback UI for recovery/debugging, but the normal product path should use public APIs.

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

If the answer points to product behavior, build it as bundled Lua instead of Go core.
