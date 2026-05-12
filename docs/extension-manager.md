# Extension manager architecture

This document defines the extension manager we want to build. It is the stable architecture contract for installing, locking, loading, updating, and diagnosing librecode extensions.

The core decision:

> Extensions are explicit dependencies, not arbitrary files that happen to exist on disk.

The stock librecode UI remains Go-owned and fast. Extensions are trusted optional code loaded only when the user/project explicitly declares them.

## Goals

- Make extension loading predictable and reproducible.
- Support first-party, GitHub-hosted, and local development extensions.
- Keep Lua as one runtime adapter behind a runtime-neutral extension host.
- Avoid embedding official extensions in the binary.
- Avoid auto-executing random directories under `.librecode/extensions`.
- Provide commands that feel like a package manager, not a manual copy workflow.

## Non-goals

- Sandboxing extension code.
- Moving the stock chat UI into Lua.
- Auto-loading every extension present on disk.
- Designing a Lua-only architecture that blocks future shell/toolbox/MCP/mvm-style runtimes.

## Configuration

Extensions are configured through `extensions.use`.

```yaml
extensions:
  enabled: true
  use:
    - official:vim-mode
    - github:example/librecode-extension
    - github:example/monorepo//extensions/fancy
    - path:.librecode/extensions/local-dev

    - source: official:vim-mode
      version: v0.1.0
    - source: github:example/librecode-extension
      version: v1.2.3
```

Supported forms:

- string source: `official:vim-mode`
- object source: `{ source: github:user/ext, version: v1.2.3 }`

Supported schemes:

- `official:<name>` — first-party extension alias.
- `github:<owner>/<repo>` — extension at repository root.
- `github:<owner>/<repo>//<subdir>` — extension inside a repository subdirectory.
- `path:<path>` — local extension directory or Lua file.

No `local:` scheme is supported. Use `path:` for local filesystem extensions.

## Explicit loading rule

At app startup, librecode loads only entries declared in `extensions.use`.

It must not recursively auto-load all directories under:

- `./.librecode/extensions`
- `~/.librecode/extensions`
- any installed extension cache directory

This rule prevents stale clones, experimental worktrees, or removed extensions from executing unexpectedly.

## Install locations

Global extension manager state lives under librecode home:

```text
~/.librecode/
  config.yaml
  extensions-lock.yaml
  extensions/
    store/
      github.com/<owner>/<repo>/...
      official/<name>/...
```

Project-local extension config and lock can live under:

```text
./.librecode/
  config.yaml
  extensions-lock.yaml
  extensions/
    local-dev-extension/
```

Project config wins over global config when it is present, matching the broader librecode config model.

## Lockfile

The lockfile pins resolved extension versions/tags. It is human-readable and version-first, not commit-first.

Recommended shape:

```yaml
extensions:
  official:vim-mode:
    resolved: github:omarluq/librecode//extensions/vim-mode
    version: v0.1.0

  github:example/librecode-extension:
    version: v1.2.3
```

Field meanings:

- key: original configured source.
- `resolved`: canonical source when the configured source is an alias like `official:*`.
- `version`: resolved tag/version to install.

The initial implementation should not require commit hashes. A future implementation may add a checksum or commit for verification, but the primary user-facing lock should stay tag/version based.

## Commands

The core command set:

```bash
librecode extension list
librecode extension add <source> [--version vX.Y.Z]
librecode extension remove <source-or-name>
librecode extension install
librecode extension update
librecode extension tidy
librecode extension doctor
```

### `list`

Shows configured, installed, loaded, errored, and disabled extensions.

Expected columns:

- name
- source
- version
- status
- runtime
- entry
- diagnostics/errors

### `add`

Adds an entry to `extensions.use`, installs it immediately, and updates the lockfile.

Example:

```bash
librecode extension add official:vim-mode
librecode extension add github:example/librecode-extension --version v1.2.3
```

### `remove`

Removes an entry from `extensions.use`, runs tidy immediately, and updates the lockfile.

Example:

```bash
librecode extension remove vim-mode
librecode extension remove official:vim-mode
```

### `install`

Reconciles config + lock into installed extension files.

Rules:

- honor existing lockfile versions when present;
- install missing locked extensions;
- resolve and lock missing versions for configured entries;
- never update already locked versions unless `update` is requested.

### `update`

Resolves newer versions/tags for configured non-`path:` entries, installs them, and rewrites the lockfile.

Initial implementation can update all extensions. Later, support:

```bash
librecode extension update vim-mode
```

### `tidy`

Removes installed extension directories that are no longer referenced by config or lock.

### `doctor`

Validates extension config, lockfile, installed directories, manifests, runtime compatibility, and load errors.

## Official registry

Official extensions are aliases resolved by librecode.

Initial registry:

```yaml
official:
  vim-mode:
    source: github:omarluq/librecode//extensions/vim-mode
    version: v0.1.0
```

The registry can start as a small Go map. Later it may be fetched from `librecode.sh` or from release metadata.

Official extensions are not embedded into the binary. They are installed like any other extension.

## Directory extension format

A directory extension has an `init.lua` manifest.

```text
my-extension/
  init.lua
  main.lua
  helpers.lua
```

Manifest:

```lua
return {
  name = "my-extension",
  version = "0.1.0",
  api_version = "v1alpha1",
  description = "Example extension.",
  entry = "main.lua",
}
```

The manifest should stay small and declarative. Behavior belongs in the entry file and sibling modules.

## Runtime adapter boundary

The extension manager installs and resolves extensions. The extension host loads them through runtime adapters.

Current runtime:

- Lua adapter

Future runtimes:

- shell hooks
- toolbox executables
- MCP-backed tool collections
- experimental Go-like runtime adapter

The manager should not know about Lua internals beyond manifest/runtime selection. Runtime-specific loading belongs in the adapter.

## Startup flow

1. Load config.
2. If extensions are disabled or `--no-extensions` is set, skip extension loading.
3. Read `extensions.use`.
4. Resolve each source:
   - `path:` directly to filesystem;
   - `official:` through the official registry and lock;
   - `github:` through install store and lock.
5. Load only resolved entries.
6. Report load diagnostics through `extension list` and logs.

## Failure policy

Extensions are trusted, but failures should not silently corrupt the app.

- Invalid config should fail validation with a clear message.
- Missing installed remote extensions should tell the user to run `librecode extension install`.
- Invalid manifests should show in `extension doctor` and `extension list`.
- Runtime errors should disable that extension for the current load and keep librecode usable.

## Acceptance criteria

The extension manager is ready when:

- `extensions.use` is the only source of loaded extensions.
- `path:`, `official:`, and `github:` parse consistently in string and object forms.
- `librecode extension add/remove/install/update/tidy/doctor/list` exist.
- `add` installs immediately and updates config + lock.
- `remove` tidies immediately and updates config + lock.
- `install` is deterministic from config + lock.
- `update` updates versions/tags intentionally.
- official `vim-mode` can be installed without embedding it in the binary.
- extension loading remains off the hot render path unless an extension is explicitly active.
