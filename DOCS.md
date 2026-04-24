# Lore Docs

Lore is a single Go binary for shared agent memory, reusable skills, project context, MCP access, and browser-admin workflows.

The primary product story is **cloud-first runtime surfaces**:

- `lore serve` for HTTP APIs, `/mcp`, and browser-admin access
- `lore mcp` for stdio MCP
- PostgreSQL for shared/cloud deployments

SQLite and `lore tui` remain supported for local development, testing, demos, and compatibility workflows.

## Docs Index

- [Installation](docs/INSTALLATION.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Agent Integration Primitives](docs/AGENT-SETUP.md)
- [Integration Ownership](docs/PLUGINS.md)
- [Comparison](docs/COMPARISON.md)
- [Security](SECURITY.md)

## Runtime Surfaces

### `lore serve`

Starts the shared runtime:

- HTTP API
- `/mcp` endpoint
- browser-admin surface

Port precedence:

1. positional arg
2. `LORE_PORT`
3. `PORT`
4. `7437`

### `lore mcp`

Starts MCP over stdio for any MCP-compatible agent.

Example:

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

Tool profiles:

- `agent`
- `admin`
- `all` (default)

### `lore tui`

Local terminal UI for browsing memories and sessions.

Use it when you want a local SQLite/dev interface. Do not treat it as the primary setup or cloud management workflow.

## Storage Modes

### Shared/cloud runtime

Set `DATABASE_URL` to a PostgreSQL connection string and run `lore serve` in your host environment.

### Local mode

Leave `DATABASE_URL` unset and Lore uses SQLite in `~/.lore` (or `LORE_DATA_DIR`).

## Environment Variables

### Core runtime

| Variable | Description | Default |
| --- | --- | --- |
| `LORE_DATA_DIR` | Local SQLite data directory | `~/.lore` |
| `LORE_PORT` | Preferred port for `lore serve` | `7437` |
| `PORT` | Cloud-host fallback port | unset |
| `DATABASE_URL` | PostgreSQL for shared runtime; otherwise SQLite remains active | unset |
| `LORE_PROJECT` | Override MCP project detection | auto-detected |

### Runtime/admin configuration

| Variable | Description |
| --- | --- |
| `LORE_ENV` | `local` or `staging` |
| `LORE_HOST` | Override bind host |
| `LORE_BASE_URL` | Public base URL for hosted/staging runtime |
| `LORE_JWT_SECRET` | Required in staging; generated per-process in local mode if unset |
| `LORE_COOKIE_SECURE` | Override secure-cookie behavior |
| `LORE_GOOGLE_CLIENT_ID` / `LORE_GOOGLE_CLIENT_SECRET` | Optional Google auth |
| `LORE_GITHUB_CLIENT_ID` / `LORE_GITHUB_CLIENT_SECRET` | Optional GitHub auth |

Staging rules:

- `LORE_ENV=staging` requires `LORE_BASE_URL`
- `LORE_ENV=staging` requires `LORE_JWT_SECRET`
- default host becomes `0.0.0.0`

## CLI Reference

```text
lore serve [port]                       Start HTTP API server
lore mcp [--tools=PROFILE] [--project] Start MCP server (stdio)
lore tui                                Launch local terminal UI
lore search <query>                     Search memories
lore save <title> <msg>                 Save a memory
lore timeline <obs_id>                  Show chronological context
lore context [project]                  Show recent context
lore stats                              Show memory system statistics
lore export [file]                      Export memories to JSON
lore import <file>                      Import memories from JSON
lore sync                               Export/import chunked sync data
lore projects list                      List project counts
lore projects consolidate               Merge similar project names
lore projects prune                     Remove empty projects
lore setup [agent]                      Deprecated compatibility stub only
lore obsidian-export                    Export to an Obsidian-compatible vault
lore version                            Print version
lore help                               Show help
```

## HTTP and MCP Contracts

### HTTP API

`lore serve` exposes JSON APIs for sessions, observations, prompts, search, timeline, context, import/export, and project management.

### MCP

Lore supports:

- stdio MCP via `lore mcp`
- HTTP MCP via `/mcp` when `lore serve` is running

These are the stable Lore-owned primitives that external configurators should target.

## Local Compatibility Surfaces

These remain supported, but secondary:

- SQLite default storage for local installs
- `lore tui` for local browsing
- `obsidian-export` and the standalone Obsidian client package

## Deprecated Vendor Setup

`lore setup [agent]` now exists only to avoid abrupt breakage for older workflows. It prints a deprecation handoff and performs no vendor installation, plugin copying, or agent-specific file writes.

Lore does **not** own:

- vendor plugin packaging
- Claude/OpenCode/Gemini/Codex installer flows
- prompt injection or hook wiring for third-party clients

Those concerns belong to external configurators or the agent client itself.

## Suggested Onboarding Paths

### Shared runtime / team deployment

1. Install Lore
2. Set `DATABASE_URL`, `LORE_BASE_URL`, and `LORE_JWT_SECRET`
3. Run `lore serve`
4. Connect agents through `/mcp` or `lore mcp`

### Local development / evaluation

1. Install Lore
2. Run `lore serve` or `lore mcp`
3. Optionally use `lore tui`
4. Keep SQLite as the local backend unless you need PostgreSQL validation

## Notes for Contributors

- Product docs must describe cloud/runtime surfaces first.
- Local SQLite/TUI guidance should stay available but explicitly secondary.
- Do not add new Lore-owned vendor setup flows or packaged agent assets.
