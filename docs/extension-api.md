# Extension Lua API

## Status

This document describes the currently implemented Lua API surface.

It is intentionally practical and code-oriented. The API is still evolving as librecode moves toward a more programmable runtime.

See also:

- `docs/adr/0001-programmable-runtime.md`
- `docs/runtime-architecture.md`
- `docs/extension-runtime.md`
- `docs/extension-roadmap.md`
- `docs/rendering-boundary.md`

## Loading model

Extensions are trusted local Lua files loaded from:

1. `extensions/`
2. configured `extensions.paths`

By default, configured extension paths are:

- `extensions`
- `.librecode/extensions`

The official bundled `extensions/` root is deduped in front of configured paths.

Each Lua file runs in its own Lua state.

Lua helper modules can live under a `lua/` subdirectory inside any extension root, or next to a loaded extension file. The extension manager adds those roots to `package.path` and skips `lua/` helper directories when discovering top-level extension files.

Example:

```text
extensions/
  vim-mode.lua
  lua/
    librecode/
      chat.lua
```

Then extensions can do:

```lua
local chat = require("librecode.chat")
```

Bundled helper modules are convenience wrappers over primitive APIs. They are not a separate Go host API family.

## Importing the API

Extensions can either use the global `librecode` table or require the module explicitly:

```lua
local lc = require("librecode")
```

## Bundled helper modules

The official extension distribution may include Lua helper modules under `extensions/lua/librecode/`. These modules are intentionally implemented in Lua on top of primitive APIs.

Current helpers include:

- `librecode.chat`
- `librecode.composer`
- `librecode.statusline`

They are convenience layers, not kernel primitives. Prefer documenting reusable product behavior there instead of adding product-specific Go APIs.

## Top-level API

## `librecode.on(event_name, fn)`

Registers a low-level event handler.

```lua
local lc = require("librecode")

lc.on("prompt_submit", function(ev)
  lc.event.consume()
  lc.buf.append("transcript", {
    kind = "message",
    role = "custom",
    text = "extension intercepted submit\n",
  })
end)
```

Variant with priority:

```lua
lc.on("key", { priority = 100 }, function(ev)
  return true
end)
```

Current commonly emitted events include:

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
- `tick`
- `before_agent_start`
- `agent_end`

## `librecode.log(message)`

Writes a message through the Go logger.

```lua
lc.log("hello from extension")
```

## `librecode.register_command(name, description, fn)`

Registers an extension command.

```lua
lc.register_command("hello", "print hello", function(args)
  return "hello " .. (args or "")
end)
```

The command appears in `librecode extension list` and can be executed with `librecode extension run <name>`.

## `librecode.register_tool(name, description, fn)`

Registers an extension-backed tool callable by the runtime.

```lua
lc.register_tool("echo_json", "returns the provided args", function(args)
  return {
    content = "ok",
    details = args,
  }
end)
```

Handlers receive a Lua table converted from Go `map[string]any` arguments.

They may return either:

- a scalar/string-like value, which becomes tool content; or
- a table with:
  - `content`
  - `details`

## `librecode.api`

Neovim-inspired low-level helpers.

### `librecode.api.create_namespace(name)`

Returns a stable numeric namespace ID for the provided name.

```lua
local ns = lc.api.create_namespace("my-extension")
```

If called again with the same name, returns the same ID.

### `librecode.api.create_autocmd(events, opts_or_fn)`

Registers event handlers for one or more event names.

Examples:

```lua
lc.api.create_autocmd("prompt_submit", function(ev)
  lc.log("submit seen")
end)

lc.api.create_autocmd({ "before_agent_start", "agent_end" }, {
  priority = 50,
  callback = function(ev)
    lc.log("lifecycle event")
  end,
})
```

Also available as:

- `librecode.api.nvim_create_autocmd`
- `librecode.autocmd.create`

### `librecode.api.create_user_command(name, opts_or_fn)`

Registers a user command.

```lua
lc.api.create_user_command("Hello", {
  desc = "say hello",
  callback = function(args)
    return "hello"
  end,
})
```

Also available as:

- `librecode.api.nvim_create_user_command`
- `librecode.command.create`

## `librecode.keymap`

### `librecode.keymap.set(target, lhs, fn, opts)`

Registers a keymap against a generic target.

Examples:

```lua
lc.keymap.set({ role = "composer" }, "ctrl+j", function(ev)
  lc.buf.set_text("status", "ctrl+j pressed")
  return true
end, { priority = 100, desc = "example role keymap" })

lc.keymap.set({ buffer = "composer" }, "*", function(ev)
  lc.log("composer key: " .. ev.key)
end)

lc.keymap.set("global", "ctrl+p", function()
  lc.action.run("transcript.tree")
  return true
end)
```

Supported target forms:

- `"global"` for global keymaps
- `{ buffer = "name" }` for buffer-scoped keymaps
- `{ window = "name" }` for window-scoped keymaps
- `{ role = "name" }` for role-scoped keymaps
- `{ scope = "buffer" | "window" | "role", name = "name" }`
- a list of any of the above

String targets other than `"global"` are kept only as a legacy shorthand and match buffer/window role names. New extensions should prefer explicit target tables.

Notes:

- `lhs` is normalized internally (`<c-j>`, `ctrl-j`, and `ctrl+j` normalize together)
- `"*"` or `"any"` matches any key
- higher `priority` runs first
- returning `true` marks the event consumed

### `librecode.keymap.del(target, lhs)`

Removes matching keymaps previously registered by the same extension.

All mutable runtime APIs currently work only during active event handling. Calling them outside an event raises an error.

## `librecode.win`

Window APIs expose and mutate the currently visible runtime windows for the active event.

### `librecode.win.list()`

Returns visible window names.

### `librecode.win.get(name)`

Returns the named window table or `nil`.

Fields currently include:

- `name`
- `role`
- `buffer`
- `renderer`
- `x`
- `y`
- `width`
- `height`
- `cursor_row`
- `cursor_col`
- `visible`
- `metadata`

`renderer = "default"` means the stock Go renderer may draw the window. `renderer = "extension"` means the extension owns the window until a later window/layout mutation changes it, so the stock renderer skips that window.

### `librecode.win.find(opts)`

Finds the first matching window.

Supported filters today:

- `name`
- `role`
- `buffer`

Example:

```lua
local win = librecode.win.find({ role = "composer" })
local buf = librecode.win.get_buf(win)
```

### `librecode.win.get_buf(name)`

Returns the buffer name displayed by the given window.

This is the current path for extensions that want to discover the composer through the visible runtime model instead of assuming a hardcoded buffer name.

### `librecode.win.set_buf(name, buffer_name)`
### `librecode.win.set_renderer(name, renderer)`
### `librecode.win.create(name[, value])`
### `librecode.win.set(name, value)`
### `librecode.win.delete(name)`

Mutate the active window set. Window mutations are applied back to the terminal runtime after the event.

Use `librecode.win.set_renderer(name, "extension")` or set `window.renderer = "extension"` before `win.set`/`layout.set` when an extension wants to fully own that window's drawing. Set it back to `"default"` to return drawing to the stock renderer.

## `librecode.layout`

Layout APIs expose the current screen dimensions and window table.

### `librecode.layout.get()`

Returns a table:

```lua
{
  width = 120,
  height = 40,
  windows = {
    composer = { role = "composer", buffer = "composer", x = 0, y = 32, width = 120, height = 6 },
  },
}
```

### `librecode.layout.set(layout)`

Replaces the runtime layout with the provided table. This is intentionally low-level: callers are responsible for non-overlap, bounds, and visibility.

## `librecode.ui`

Low-level drawing APIs enqueue window-relative draw operations for the current frame/event.

Current UI primitives are intentionally small. They are enough for simple windows like the bundled statusline and Vim composer, but not yet enough to faithfully reimplement complex hot renderers such as the transcript. See `docs/rendering-boundary.md` for the current rendering boundary.

### `librecode.ui.measure(text)`

Returns terminal display width in cells using the Go rendering backend's grapheme-aware width logic.

### `librecode.ui.truncate(text, width)`

Returns text truncated to fit `width` cells. Truncation is grapheme-aware and appends an ellipsis when possible.

### `librecode.ui.pad_right(text, width)`

Returns text padded/truncated to exactly `width` cells.

### `librecode.ui.wrap(text, width)`

Returns a list of wrapped lines using the same generic Go-backed wrapping logic used by stock renderers.

### `librecode.ui.clear_window(name)`
### `librecode.ui.draw_text(name, row, col, text[, style])`
### `librecode.ui.draw_lines(name, row, col, lines[, style])`
### `librecode.ui.draw_spans(name, row, col, spans)`
### `librecode.ui.draw_box(name[, style])`
### `librecode.ui.set_cursor(name, row, col)`

`draw_spans` accepts inline spans such as:

```lua
{
  { text = "hot", fg = "accent", bold = true },
  { text = " cold", fg = "dim" },
}
```

Example:

```lua
lc.on("render", function()
  local win = lc.win.find({ role = "composer" })
  if not win then return end
  lc.win.set_renderer(win, "extension")
  lc.ui.clear_window(win)
  lc.ui.draw_box(win, { fg = "border" })
  lc.ui.draw_spans(win, 1, 2, {
    { text = "custom ", fg = "text" },
    { text = "composer", fg = "accent", bold = true },
  })
  lc.ui.set_cursor(win, 1, 2)
end)
```

Still-planned generic UI primitives include clipping helpers, theme/highlight resolution, viewport helpers, virtual-list helpers, and richer batched draw operations.

## `librecode.buf`

### `librecode.buf.list()`

Returns visible buffer names for the current event.

### `librecode.buf.create(name[, value])`

Creates or replaces a buffer in the current event result.

```lua
lc.buf.create("notes", { text = "hello", cursor = 5 })
```

### `librecode.buf.delete(name)`

Marks a buffer for deletion.

### `librecode.buf.get(name)`

Returns a table containing the current buffer state.

Fields include:

- `name`
- `text`
- `chars`
- `cursor`
- `label`
- `metadata`

### `librecode.buf.get_text(name)`
### `librecode.buf.set_text(name, text)`

Get or replace buffer text.

### `librecode.buf.get_cursor(name)`
### `librecode.buf.set_cursor(name, cursor)`

Get or replace cursor position.

### `librecode.buf.insert(name, position, text)`
### `librecode.buf.delete_range(name, start, end)`
### `librecode.buf.replace(name, start, end, replacement)`

Rune-oriented editing helpers for low-level buffer mutation.

### `librecode.buf.get_lines(name, start, end)`
### `librecode.buf.set_lines(name, start, end, lines)`

Line-oriented helpers for replacing a range.

### `librecode.buf.set(name, value)`

Replace the full buffer state.

### `librecode.buf.append(name, value)`

Append text or one structured block to a buffer.

Examples:

```lua
lc.buf.append("status", "working")

lc.buf.append("transcript", {
  kind = "message",
  text = "tool finished\n",
  role = "tool_result",
})
```

String values append to buffer text. Tables with block fields append to `buffer.blocks`.

### `librecode.buf.clear(name)`

Clear buffer text, blocks, and cursor.

### `librecode.buf.get_blocks(name[, start[, end]])`
### `librecode.buf.set_blocks(name, start, end, blocks)`
### `librecode.buf.delete_blocks(name, start, end)`

Structured block helpers. Blocks are generic and may be used for transcript items, tool output, annotations, or any extension-defined data.

Recognized block fields include:

- `id`
- `kind`
- `role`
- `text`
- `index`
- `created_at`
- `streaming`
- `metadata`

### `librecode.buf.get_var(name, key)`
### `librecode.buf.set_var(name, key, value)`

Read or write buffer metadata values.

## `librecode.action`

Request host-side runtime actions from an extension.

### `librecode.action.run(name)`

Current built-ins include:

- `submit`
- `history.prev`
- `history.next`
- `autocomplete.accept`
- `followup.queue`
- `followup.dequeue`
- `interrupt`
- `prompt.cancel`
- `transcript.tree`

## `librecode.timer`

Timer callbacks run at the start of the next terminal runtime event whose clock is past the scheduled time. They execute inside the same transaction model as event handlers, so they may use `buf`, `win`, `layout`, `ui`, and `action` APIs.

### `librecode.timer.defer(ms, fn)`

Schedules a one-shot callback and returns a timer ID.

### `librecode.timer.interval(ms, fn)`

Schedules a repeating callback and returns a timer ID.

### `librecode.timer.stop(id)`

Cancels a pending timer.

## `librecode.event`

These helpers only make sense during active event execution.

### `librecode.event.consume()`

Marks the event as consumed.

### `librecode.event.stop()`

Marks the event as consumed and stops later handlers.

## Event payload shape

Handlers for terminal events receive a table like:

```lua
{
  name = "key",
  key = "ctrl+j",
  text = "",
  ctrl = true,
  alt = false,
  shift = false,
  working = false,
  auth_working = false,
  context = {
    mode = "chat",
    working = false,
    auth_working = false,
    cwd = "/path/to/project",
    session_id = "abc",
  },
  data = {
    text = "incremental event text",
  },
  composer = { text = "hello", cursor = 5, chars = { "h", "e", "l", "l", "o" }, metadata = {} },
  buffers = {
    composer = { text = "hello", cursor = 5, chars = { "h", "e", "l", "l", "o" }, blocks = {}, metadata = {} },
    status = { text = "", cursor = 0, chars = {}, blocks = {}, metadata = {} },
    transcript = {
      text = "",
      cursor = 0,
      chars = {},
      metadata = { count = 12, snapshot_count = 12, snapshot_start = 0, snapshot_limit = 32 },
      blocks = {
        { id = "message:0", kind = "message", role = "user", text = "hello", index = 0, streaming = false },
        { id = "streaming:11", kind = "streaming", role = "assistant", text = "partial", index = 11, streaming = true },
      },
    },
    thinking = { text = "", cursor = 0, chars = {}, blocks = {}, metadata = { count = 1 } },
    tools = { text = "", cursor = 0, chars = {}, blocks = {}, metadata = { count = 1 } },
  },
}
```

## Built-in example

The bundled `extensions/vim-mode.lua` shows how to build substantial behavior using the current API.

It demonstrates:

- event-driven state
- keymaps
- composer editing
- label updates
- low-level buffer mutation

## Current limitations

The API is still incomplete compared with the long-term target.

Notably missing today:

- job/process spawning primitives
- richer Lua helper modules for chat/transcript/status convenience
- highlights/extmarks/namespaced annotations
- deeper assistant/model/tool runtime replacement hooks

Future APIs should preserve the primitive-first boundary described in `docs/runtime-architecture.md`: add kernel primitives to Go, and build product convenience in Lua.
