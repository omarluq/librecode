# ADR 0001: librecode as a programmable runtime

- Status: accepted
- Date: 2026-05-07

## Context

librecode started as a local-first AI coding assistant with a terminal chat UI, persistent sessions, built-in tools, skills, and trusted Lua extensions.

That model is no longer enough for the direction of the project.

The goal is not only to support plugins that customize a fixed chat application. The goal is to turn librecode into a programmable runtime where the bundled chat experience is the default implementation, not the only implementation.

Users should be able to control any and all buffers, event flow, and runtime behavior. They should be able to reskin the UI, replace interaction patterns, intercept or replace the assistant loop, and build workflows that only partially resemble the default product.

This follows a Unix-style trust model:

- extensions are local and trusted
- extensions are allowed to footgun
- the core should preserve internal invariants and avoid deadlocks/corruption
- the core should not try to sandbox trusted local code

## Decision

librecode will evolve toward a low-level extension architecture inspired by systems such as Neovim:

- expose primitives instead of product-specific plugin hooks
- model visible UI state as buffers and events
- let extensions observe, mutate, consume, and eventually replace default behavior
- keep the bundled chat UI implemented on top of the same public primitives over time

The extension system will prioritize small composable building blocks:

- buffers
- windows
- layout
- UI drawing
- keymaps
- commands
- autocmd-like events
- namespaces
- runtime events
- jobs/timers
- model/tool/session primitives

Product-specific convenience should be built in Lua modules on top of those primitives. The Go host should avoid growing narrow APIs such as transcript/composer/thinking helpers unless they are clearly temporary compatibility surfaces.

## Consequences

### Positive

- librecode can become a programmable editor/runtime instead of a narrow chat app
- official features can be built using the same public API as user extensions
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
3. Move more core terminal state onto the shared buffer/event model.
4. Add render and layout primitives so extensions can replace the default UI.
5. Add runtime hooks so extensions can override more of the assistant/model/tool flow.
6. Gradually rebuild bundled behaviors on top of the public runtime API.

## Notes

This ADR establishes the direction. The detailed shape of the runtime lives in:

- `docs/runtime-architecture.md`
- `docs/extension-runtime.md`
- `docs/extension-roadmap.md`
- `docs/extension-api.md`
