# Rendering boundary

## Status

Accepted direction for the extension runtime.

This document records the lesson from the first transcript-rendering migration attempt: Lua is the right control plane for librecode, but the Go runtime must remain the fast rendering backend and provide stronger generic rendering primitives before complex UI surfaces move to Lua.

Related docs:

- [`runtime-architecture.md`](runtime-architecture.md)
- [`extension-runtime.md`](extension-runtime.md)
- [`extension-roadmap.md`](extension-roadmap.md)
- [`extension-api.md`](extension-api.md)

## Decision

librecode will keep Lua as the product/control layer and Go as the runtime/rendering kernel.

Lua should decide:

- which windows exist
- which buffers are shown
- which window owns rendering
- what blocks, spans, or UI elements should be drawn
- how keymaps, modes, statuslines, layouts, and assistant policy behave

Go should provide:

- terminal I/O and screen flushing
- clipping, measuring, wrapping, and style application
- efficient draw batching
- viewport/virtual-list helpers
- cached rendering for large histories
- core invariants and bounded work in hot paths

The goal is not to make Lua manually reimplement every pixel/string rule from the existing Go renderer. The goal is to let Lua compose high-level product behavior while Go exposes sharp, generic, efficient primitives.

## Why this boundary exists

Simple UI features migrated well:

- Vim composer behavior is mostly Lua-owned.
- Vim composer rendering works because the surface is small and local.
- Statusline rendering works because it is a few short lines.

The transcript migration did not meet the quality bar because transcript rendering is a hot and complex UI surface. It includes:

- wrapping and measuring text
- spacing and grouping message blocks
- thinking/tool styling
- streaming block ordering
- scrollback and viewport slicing
- color and border consistency
- cached rendering for large histories
- performance during high-frequency model/thinking deltas

The existing Go path already handles many of those concerns. Moving that logic wholesale into Lua before the runtime exposes better generic rendering primitives causes visual regressions and render-loop slowdowns.

## Current rule

Do not move complex hot renderers to Lua just because a window can be extension-owned.

A renderer is ready to migrate only when either:

1. the Lua implementation can match the Go renderer's visual and interaction behavior with existing primitives, or
2. missing generic primitives are added to the Go kernel first.

Until then, keep the mature Go renderer and expose bounded, generic data to Lua.

## Primitive-first migration

When a Lua renderer cannot match the stock renderer, do **not** add product-specific host APIs like `transcript.render()` or `thinking.draw()`.

Instead, add generic primitives that are useful outside chat:

- `ui.measure(text[, opts])`
- `ui.wrap(text, width[, opts])`
- `ui.truncate(text, width[, opts])`
- `ui.draw_lines(window, row, col, lines[, style])`
- `ui.draw_spans(window, row, col, spans)`
- `ui.draw_box(window, opts)`
- `ui.clear_region(window, row, col, height, width)`
- `ui.clip(window, fn)` or explicit clipped draw operations
- named highlight groups and theme token resolution
- namespace-scoped highlights/extmarks/virtual text
- window viewport/scroll helpers
- virtual-list helpers for large block lists
- batched draw operations

These primitives keep Go responsible for the parts that require performance and terminal correctness while still letting Lua own product decisions.

## Render ownership levels

A window can be in one of three practical states:

1. **Stock-rendered** — Go renders the default UI for that window.
2. **Extension overlay** — Go renders the stock UI, and Lua draws additional UI on top.
3. **Extension-owned** — Lua marks `renderer = "extension"`; Go skips stock rendering for that window and only applies extension draw operations.

Extension-owned rendering should be used carefully for hot/complex windows until the primitive set is strong enough.

## Transcript policy

Transcript rendering stays Go-owned for now.

Lua may inspect bounded transcript data through generic buffer blocks and metadata, but the stock transcript renderer remains in Go until render parity is achievable.

Specifically:

- keep transcript snapshots bounded by block count and text length
- do not rebuild full transcript text on every render or streaming delta
- do not add transcript-specific host write/render APIs
- add generic buffer/UI/viewport primitives instead
- maintain a render parity checklist before retrying a Lua transcript renderer

## Render parity checklist

Before migrating a complex stock renderer to Lua, verify at least:

- visual spacing matches the Go renderer
- borders and text colors do not bleed
- wide/UTF-8 characters measure correctly
- wrapping matches terminal width
- thinking text remains dim/italic where intended
- tool blocks keep their current styling and expansion behavior
- streaming thinking/tool/answer blocks remain chronological
- scrolling remains smooth with long sessions
- render work is bounded by visible rows, not full history size
- high-frequency model/thinking deltas do not trigger unbounded Lua work
- tests cover small, large, streaming, and scrolled transcripts

## Near-term direction

The next rendering work should continue strengthening generic primitives rather than migrate transcript wholesale.

Implemented first-pass Go-backed primitives:

- `ui.measure`
- `ui.truncate`
- `ui.pad_right`
- `ui.wrap`
- `ui.draw_lines`
- `ui.draw_spans`
- `ui.draw_box`
- `ui.clear_region`
- `ui.viewport`

Recommended remaining order:

1. Add theme/highlight token resolution for stable colors across renderers.
2. Add virtual-list helpers for bounded rendering of large block lists.
3. Add richer batched draw operations if render events become chatty.
4. Only then retry Lua-owned transcript rendering behind a feature flag or experimental extension.

## What still belongs in Lua now

Lua is still the right place for:

- Vim mode behavior and composer rendering
- statusline content/rendering
- simple panels and overlays
- layout decisions
- keymaps and prompt history UX
- assistant policy hooks as primitives mature
- convenience modules over `buf`, `win`, `layout`, `ui`, and `event`

The boundary is not "Go UI versus Lua UI". The boundary is:

> Lua owns product policy and composition; Go owns hot, generic terminal primitives.
