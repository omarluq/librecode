# ADR 0001: librecode as a programmable runtime

- Status: accepted
- Date: 2026-05-07

## Context

librecode started as a local-first AI coding assistant with a terminal chat UI, persistent sessions, built-in tools, skills, and trusted Lua extensions.

That model is no longer enough for the direction of the project.

The goal is to support powerful local customization without sacrificing the polished default chat application. librecode should be programmable, but the default experience should remain Go-owned and excellent with extensions disabled.

Users should be able to inspect and mutate buffers, event flow, and runtime behavior through trusted extensions. They should be able to add keymaps, commands, overlays, custom workflows, and opt-in UI replacements without making Lua mandatory for the default product.

This follows a Unix-style trust model:

- extensions are local and trusted
- extensions are allowed to footgun
- the core should preserve internal invariants and avoid deadlocks/corruption
- the core should not try to sandbox trusted local code

## Decision

librecode will evolve toward a low-level extension architecture inspired by systems such as Neovim:

- expose primitives instead of product-specific plugin hooks
- model visible UI state as buffers and events
- let extensions observe, mutate, consume, and optionally replace behavior
- keep the default chat UI in Go unless an opt-in extension deliberately overrides it

The extension system will prioritize small composable building blocks:

- buffers
- windows
- layout
- UI drawing
- Go-backed measurement/wrapping/clipping/batching primitives
- keymaps
- commands
- autocmd-like events
- namespaces
- runtime events
- jobs/timers
- model/tool/session primitives

Product-specific extension convenience can be built in Lua modules on top of those primitives. The Go host should avoid growing narrow extension APIs such as transcript/composer/thinking helpers unless they are clearly kernel actions or temporary compatibility surfaces.

Lua is the optional control/customization layer, not the default product implementation or hot rendering engine. Complex renderers should stay in Go by default; optional Lua renderers should use generic Go-backed primitives when parity is achievable.

## Consequences

### Positive

- librecode can become a programmable editor/runtime instead of a narrow chat app
- users can fully reskin or replace the terminal UX
- advanced workflows can override prompt submission, transcript handling, tool presentation, and session behavior

### Negative

- architecture becomes more abstract and requires stronger documentation
- core state must be modeled more consistently around buffers and events
- backwards compatibility for the Lua API becomes more important
- debugging extension interactions becomes more complex

## Non-goals

The project does not promise:

- a security sandbox for trusted local extensions
- a curated permission system for extension behavior
- a frozen API while the runtime primitives are still being established

## Implementation direction

The migration should happen incrementally.

1. Add low-level Lua primitives for events, buffers, keymaps, commands, and namespaces.
2. Expand the event surface so the default terminal and assistant loops expose meaningful lifecycle hooks.
3. Move more terminal state onto bounded buffer/event snapshots where useful.
4. Add render and layout primitives so extensions can overlay or replace simple UI surfaces by choice.
5. Strengthen generic rendering primitives without migrating complex hot stock surfaces by default.
6. Add runtime hooks so extensions can wrap or replace more of the assistant/model/tool flow.
7. Keep default UX in Go unless a feature is explicitly opt-in.

## Notes

This ADR establishes the direction. The detailed shape of the runtime lives in:

- `docs/runtime-architecture.md`
- `docs/extension-runtime.md`
- `docs/extension-roadmap.md`
- `docs/extension-api.md`
- `docs/rendering-boundary.md`
