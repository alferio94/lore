[← Back to README](../README.md)

# Integration Ownership

Lore does not ship or own vendor-specific plugin/configurator packages for OpenCode, Claude Code, Gemini CLI, Codex, or similar tools.

## What Lore owns

- `lore serve`
- `lore mcp`
- `/mcp`
- runtime/auth/storage configuration
- project/workspace detection hints

## What external configurators own

- vendor marketplace packaging
- hook wiring
- prompt injection
- local config-file mutation
- installer UX for specific agent clients

## In-repo integrations

### Obsidian

`plugin/obsidian/` remains in this repository because it is a standalone client integration, not a configurator that implies ownership of a vendor setup workflow.

## Deprecated setup note

`lore setup [agent]` is a compatibility-only deprecation stub. It exists to redirect legacy users without reintroducing vendor ownership into Lore core.
