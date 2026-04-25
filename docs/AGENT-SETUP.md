[← Back to README](../README.md)

# Agent Integration Primitives

Lore no longer owns vendor-specific setup flows, packaged plugins, or configurator UX.

This document describes the **stable Lore-owned primitives** that any external configurator or agent client can rely on.

- [Supported primitives](#supported-primitives)
- [MCP stdio](#mcp-stdio)
- [HTTP and `/mcp`](#http-and-mcp)
- [Runtime configuration](#runtime-configuration)
- [Project and workspace hints](#project-and-workspace-hints)
- [Deprecated setup command](#deprecated-setup-command)

---

## Supported primitives

External clients should integrate through these surfaces only:

- `lore mcp` for stdio MCP
- `lore serve` for HTTP APIs and `/mcp`
- `LORE_BASE_URL` for the public runtime URL
- `LORE_JWT_SECRET` and related auth configuration for hosted/admin flows
- `DATABASE_URL` / `LORE_DATA_DIR` for storage/runtime mode selection
- project detection via `LORE_PROJECT` or the runtime's default git-based detection

If you need agent-specific hook wiring, prompt injection, marketplace packaging, or config-file mutation, keep that logic in the external configurator.

---

## MCP stdio

Run Lore as a stdio MCP server:

```bash
lore mcp --tools=agent
```

Example config:

```json
{
  "mcp": {
    "lore": {
      "type": "stdio",
      "command": "lore",
      "args": ["mcp", "--tools=agent"]
    }
  }
}
```

Available profiles:

- `agent`
- `admin`
- `all`

---

## HTTP and `/mcp`

Run the shared runtime:

```bash
lore serve
```

This exposes:

- Lore's JSON HTTP APIs
- MCP over HTTP at `/mcp`
- browser-admin routes

For hosted/staging environments, set `LORE_BASE_URL` to the public runtime URL and present `Authorization: Bearer <jwt>` for HTTP `/mcp` access.

---

## Runtime configuration

Key variables for external configurators and operators:

| Variable | Purpose |
| --- | --- |
| `LORE_BASE_URL` | Public base URL for the shared runtime |
| `LORE_JWT_SECRET` | JWT signing secret for hosted/admin sessions |
| `LORE_BOOTSTRAP_ADMIN_EMAIL` | Bootstrap admin email |
| `LORE_BOOTSTRAP_ADMIN_PASSWORD` | Bootstrap admin password (required in staging; no default password exists) |
| `LORE_BOOTSTRAP_ADMIN_NAME` | Optional bootstrap admin display name |
| `DATABASE_URL` | Select PostgreSQL for shared runtime |
| `LORE_DATA_DIR` | Local SQLite directory override |
| `LORE_PORT` / `PORT` | Runtime port selection |
| `LORE_PROJECT` | Override project hint for MCP |
| `LORE_GOOGLE_CLIENT_ID` / `LORE_GOOGLE_CLIENT_SECRET` | Optional Google auth |
| `LORE_GITHUB_CLIENT_ID` / `LORE_GITHUB_CLIENT_SECRET` | Optional GitHub auth |

---

## Project and workspace hints

Lore resolves project context in this order:

1. `LORE_PROJECT`
2. runtime detection from the current repository/directory

External configurators may pass a project override when they need deterministic workspace routing, but project ownership remains a Lore runtime concern.

Auth and approval notes for agent operators:

- Lore uses one JWT identity format for browser sessions and HTTP `/mcp` bearer tokens.
- `/mcp` re-resolves the actor from the store on every request, so stale role claims do not override current role/status.
- Pending or disabled users receive `403` even if they still hold an older token.
- Active `developer` and `tech_lead` users get memory read/write plus skill-read tools; `admin` gets the full/admin-delete tool surface; `na` has no effective MCP tools.
- OAuth-created users are still subject to pending approval before they can use `/mcp`.

---

## Deprecated setup command

`lore setup [agent]` remains only as a compatibility/deprecation surface.

It does **not**:

- install vendor plugins
- copy packaged assets
- mutate Claude/OpenCode/Gemini/Codex settings
- write prompt files or hook scripts

Use your external configurator or manual MCP/HTTP wiring instead.
