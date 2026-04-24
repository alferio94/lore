[← Back to README](../README.md)

# Why Not claude-mem?

[claude-mem](https://github.com/thedotmack/claude-mem) is an influential project, but Lore is optimized for a different product shape.

| | **Lore** | **claude-mem** |
|---|---|---|
| **Primary surface** | Shared runtime + MCP + browser admin | Claude Code-oriented local plugin flow |
| **Agent lock-in** | None; any MCP or HTTP client | Claude Code-centered |
| **Storage** | PostgreSQL for shared runtime, SQLite for local mode | SQLite + ChromaDB |
| **Deployment story** | Single Go binary that can run locally or as a hosted service | Local worker/service stack |
| **Local UI** | Terminal UI for local compatibility | Web viewer on localhost |
| **What gets stored** | Agent-curated summaries | Raw tool calls + later compression |
| **Compression** | Done by the agent inline | Separate AI compression pass |
| **Dependencies** | One Go binary | Multiple runtimes and services |
| **Runtime contract** | MCP stdio, `/mcp`, HTTP APIs | Plugin/system-specific |

## Product Philosophy Difference

Lore is built around a shared runtime boundary:

- a hosted or shared service can expose HTTP APIs and `/mcp`
- the same binary can still run locally with SQLite
- agents and external configurators consume stable Lore-owned primitives instead of repo-packaged vendor installers

SQLite and `lore tui` remain useful, but they are positioned as local convenience surfaces rather than the full product identity.

By contrast, claude-mem is strongly shaped around Claude-specific plugin workflows and an always-on local capture/compression model.

## Why this matters

This tradeoff gives Lore:

- one runtime contract for browser admins, MCP clients, and HTTP integrations
- simpler ownership boundaries for external configurators
- a path to multi-user/shared deployments without changing products
- a local mode that still works for development, tests, and demos

---

Inspired by [claude-mem](https://github.com/thedotmack/claude-mem), but optimized for a cloud-first, agent-agnostic runtime model.
