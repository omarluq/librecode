# Extension runtime roadmap

This roadmap turns the programmable-runtime architecture into concrete engineering phases.

The guiding principle is simple:

> Keep the default product polished in Go. Expose sharp Lua primitives for optional customization.

## Current checkpoint

The runtime currently has:

- trusted Lua extension loading
- commands and extension tools
- event handlers/autocmds with priority, consume, and stop
- generic keymap targets by buffer/window/role/global
- runtime buffers
- runtime windows
- layout get/set
- low-level UI draw/cursor operations
- render and resize events
- per-window renderer ownership
- canonical composer buffer
- generic structured buffer blocks with bounded transcript block exposure

This is a strong foundation. Go intentionally owns stock chat rendering and assistant orchestration by default.

Important boundary: Lua is an optional control/customization layer; Go remains the product core and fast terminal rendering backend. Complex hot renderers should not migrate to Lua by default.

## Immediate cleanup

### 1. Do not add transcript-specific host writes

Do not merge or reintroduce host APIs like:

- `librecode.transcript.append`
- `librecode.transcript.clear`

Use generic buffer operations instead. If convenience is needed, implement it as a Lua helper module.

### 2. Keep transcript snapshots bounded

Any transcript/message exposure during render/stream events must be bounded by count and text length.

The previous render-loop slowdown came from rebuilding too much transcript text too often. Avoid repeating that mistake.

### 3. Treat `composer`, `transcript`, `status`, `thinking`, and `tools` as conventional default buffers

They are useful names and roles, but they should not become special host APIs.

## Phase 1: generic structured buffers

Status: implemented for the current runtime surface.

Goal: make transcript/message-like data possible without transcript-specific Go APIs.

Completed generic operations:

- `buf.clear(name)`
- `buf.append(name, value)` for text or structured blocks
- `buf.get_blocks(name, start, stop)`
- `buf.set_blocks(name, start, stop, blocks)`
- `buf.delete_blocks(name, start, stop)`
- `buf.get_var(name, key)` / `buf.set_var(name, key, value)`

Design constraints:

- operations must be bounded for render/stream events
- mutation must happen through an active event or scheduled transaction
- block schema should be generic: `kind`, `text`, `metadata`, plus optional application fields like `role`
- transcript should be one consumer of structured buffers, not its own host API family

## Phase 2: optional Lua helper modules

Goal: give users ergonomic APIs without bloating the Go kernel.

Helper modules may live under a configured extension root and wrap primitives for a project or workflow. librecode should not depend on auto-loaded bundled Lua helpers for the stock UX.

Example direction:

```lua
local chat = require("my_workflow.chat")
chat.append_note("hello")
```

Internally, this should use `buf`, `win`, `layout`, `ui`, `event`, and `action` primitives.

## Phase 3: optional UI overrides where parity is ready

Goal: let users customize the terminal UI without degrading the stock Go experience.

Keep default UI surfaces in Go:

- status/footer
- composer behavior/rendering
- prompt history UX
- autocomplete presentation
- default layout construction
- transcript/thinking/tool presentation

Allow opt-in Lua extensions to overlay or own windows when they can preserve parity and performance.

The failed transcript migration showed the boundary: Lua should not manually reimplement complex hot renderers before Go provides the right generic primitives.

## Phase 4: render model improvements

Goal: make extension rendering powerful enough for full reskins while keeping hot terminal work in Go.

First-pass implemented:

- `ui.measure`
- `ui.truncate`
- `ui.pad_right`
- `ui.wrap`
- `ui.draw_lines`
- `ui.draw_spans`
- `ui.draw_box`
- `ui.clear_region`
- `ui.viewport`
- `ui.virtual_list`
- `ui.draw_batch`
- `ui.theme_tokens`

Still add:

- namespace-scoped highlights
- extmarks/virtual text or equivalent annotations
- richer window viewport/scroll APIs
- per-window renderer registration helpers

Keep raw draw operations available. Higher-level rendering should be Lua-composable, but measuring/wrapping/caching should use Go-backed primitives.

## Phase 5: runtime lifecycle and scheduling

Goal: make Lua capable of owning long-running behavior without blocking the app.

Add events:

- `shutdown`
- `tick`
- `session_load`
- `session_save`
- `prompt_prepare`
- `model_request`
- `tool_delta`
- `message_append`
- `transcript_render`

Add primitives:

- `job.spawn`
- `job.stop`
- `timer.defer` (implemented)
- `timer.interval` (implemented)
- `timer.stop` (implemented)
- `schedule(fn)`

## Phase 6: assistant/model/tool/session primitives

Goal: allow extensions to replace or deeply reshape the assistant loop.

Expose primitive capabilities, not chat policies:

- model stream request primitive
- tool run primitive
- session read/write primitive
- config read/write primitive
- store/key-value primitive for extension state

Keep default assistant orchestration in Go; allow extensions to override or wrap it explicitly.

## Phase 7: bare runtime mode

Goal: make librecode usable as a programmable terminal runtime without the stock chat app.

Possible shape:

```bash
librecode --bare
librecode --extension ./my-app.lua
```

Bare mode should load the kernel and selected extensions, but not the default chat distribution.

## Anti-roadmap

Avoid these directions:

- a growing family of `composer.*`, `transcript.*`, `thinking.*`, or `chat.*` host APIs
- special extension hooks for one bundled feature when generic events/keymaps/buffers can handle it
- unbounded snapshots in hot render/stream paths
- hidden state mutation outside event transactions
- rewriting complex mature Go renderers in Lua before primitive parity exists
- making product policy live in the Go stock renderer instead of moving policy to Lua

It is acceptable to improve Go's generic rendering primitives; it is not acceptable to add transcript-specific rendering APIs just to paper over missing primitives.

## Definition of done for the architecture

The architecture is in the target state when:

1. The default chat UI is fast and polished with extensions disabled.
2. Extensions can customize buffers/windows/events through public primitives.
3. Optional composer modes or render overrides need no private host support.
4. Transcript, statusline, thinking, and tool presentation remain Go-owned by default but expose bounded data and opt-in override points.
5. Extensions can wrap or replace assistant flow using model/tool/session primitives.
6. Render/stream performance stays bounded regardless of session history size.
7. Complex Lua-owned renderers use Go-backed generic rendering primitives rather than ad hoc Lua string math.
