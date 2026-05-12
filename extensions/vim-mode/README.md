# vim-mode

Experimental Lua-only Vim-style composer editing for librecode.

This extension intentionally uses only the public Lua extension API. It owns
composer key handling through generic keymaps and mutates the composer buffer
directly; the Go codebase is not involved in the Vim behavior.

## Install locally from this checkout

The repository keeps this extension under `extensions/vim-mode`. To enable it
for local development, symlink it into the project extension root:

```bash
mkdir -p .librecode/extensions
ln -s ../../extensions/vim-mode .librecode/extensions/vim-mode
```

## Current coverage

- insert, normal, and visual modes
- arrows and `h/j/k/l`
- `w`, `b`, `e`, `0`, `^`, `$`, `gg`, `G`
- `i`, `a`, `I`, `A`, `o`, `O`
- `x`, `X`, `D`, `C`
- `d`, `c`, `y` operators with motions and line forms
- `p`, `P`, `u`, `ctrl+r`, `r`
- visual `y`, `d`, `c`
- `enter` submit and `tab` autocomplete accept

This is an experiment, not a complete Vim implementation.
