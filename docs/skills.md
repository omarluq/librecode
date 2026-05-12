# Agent Skills

librecode supports [Agent Skills](https://agentskills.io/specification) as local, reusable instruction bundles.

## Discovery

Default roots are checked in priority order:

1. `./.librecode/skills`
2. `./.agents/skills`
3. `~/.librecode/skills`
4. `~/.agents/skills`

A spec skill is a directory containing `SKILL.md`:

```text
.librecode/skills/my-skill/SKILL.md
```

Skill discovery uses Agent Skills-compatible directories containing `SKILL.md`.

Duplicate skill names are resolved by discovery priority. Lower-priority duplicates produce diagnostics and are ignored.

Discovery honors `.gitignore`, `.ignore`, and `.fdignore` files found in scanned skill directories. Symlinked directories are followed through normal filesystem resolution and canonical paths are used for dedupe/cycle prevention.

## Frontmatter

Supported fields:

```yaml
---
name: my-skill
description: Use when this workflow applies.
license: MIT
compatibility: Works with Agent Skills-compatible agents.
allowed-tools: Read Bash(git:*)
user-invocable: true
disable-model-invocation: false
metadata:
  author: me
  version: "0.1.0"
---
```

Validation rules:

- `name` defaults to the parent directory when omitted.
- `name` must be lowercase `a-z`, `0-9`, and hyphens only.
- `name` must be at most 64 characters.
- `name` should match the parent directory name.
- `description` is required and must be at most 1024 characters.
- `compatibility` must be at most 500 characters.
- `allowed-tools` may be either a spec string (`Read Bash(git:*)`) or a YAML string list.

## Prompt behavior

librecode uses progressive disclosure:

1. The system prompt advertises valid skill names, descriptions, and file paths.
2. The model may read a skill file when it decides the skill applies.
3. librecode also performs conservative auto-activation by matching the current user prompt against skill names and descriptions.
4. Auto-activated skills have their full `SKILL.md` content injected into the request context, bounded to protect prompt size.

Auto-activation emits a `skill_auto_activate` lifecycle event for extensions.

## Slash command

Inside chat:

- `/skill` lists discovered skills.
- `/skill <name>` prints the full `SKILL.md` for that skill.

CLI commands:

```bash
librecode skill list
librecode skill show my-skill
librecode skill validate
```

## Remaining compatibility work

The current implementation is intentionally close to the published spec. Future work should focus on:

- better ignore-pattern parity for advanced `.gitignore` syntax;
- explicit skill activation diagnostics in the TUI;
- optional enforcement/reporting of `allowed-tools` when tool permissions become first-class.
