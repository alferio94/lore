[← Back to README](../README.md)

# Architecture

- [Product Boundary](#product-boundary)
- [Runtime Topology](#runtime-topology)
- [Storage Modes](#storage-modes)
- [Core Responsibilities](#core-responsibilities)
- [Repository Structure](#repository-structure)
- [Contributor Implications](#contributor-implications)

---

## Product Boundary

Lore is a **cloud-first runtime** with local compatibility surfaces.

Primary surfaces:

- `lore serve` for HTTP APIs, `/mcp`, and browser-admin workflows
- `lore mcp` for stdio MCP
- PostgreSQL-backed shared deployments

Secondary/local-only surfaces:

- SQLite when `DATABASE_URL` is unset
- `lore tui` for local browsing, demos, tests, and development

Lore core does not own vendor-specific configurators, packaged plugins, or agent setup automation.

---

## Runtime Topology

```text
External configurator / MCP client / browser admin
                    │
          HTTP API, /mcp, or MCP stdio
                    │
                   Lore
                    │
      PostgreSQL (shared runtime) or SQLite (local mode)
```

### HTTP runtime

`lore serve` composes:

- JSON APIs
- MCP over HTTP at `/mcp`
- admin/browser routes

### MCP runtime

`lore mcp` provides stdio MCP for agent clients that do not need the HTTP surface.

---

## Storage Modes

### Shared runtime

Use PostgreSQL by setting `DATABASE_URL`.

This is the preferred product story for team/shared deployments because it gives Lore a single remotely hosted runtime surface.

### Local mode

If `DATABASE_URL` is unset, Lore uses SQLite in `~/.lore` (or `LORE_DATA_DIR`).

This mode remains important for:

- local development
- demos
- tests
- single-user evaluation
- compatibility workflows such as `lore tui`

---

## Core Responsibilities

| Area | Responsibility |
| --- | --- |
| `cmd/lore` | Runtime composition, command surfaces, env/config handling |
| `internal/server` | HTTP API and request handling |
| `internal/mcp` | MCP tool server and tool profile resolution |
| `internal/admin` | Browser-admin/auth surface |
| `internal/store` | Storage contracts and backend selection |
| `internal/project` | Project/workspace detection hints |
| `internal/tui` | Local-only terminal browser |
| `internal/sync` | Chunked export/import sync workflows |

### Deprecated compatibility boundary

`internal/setup` remains only as a compatibility seam for the deprecated `lore setup [agent]` command. It must not regain vendor installation, file-writing, or plugin-packaging behavior.

---

## Repository Structure

```text
lore/
├── cmd/lore/                 # CLI and runtime composition
├── internal/
│   ├── admin/                # Browser-admin/auth surface
│   ├── mcp/                  # MCP stdio + HTTP transport support
│   ├── project/              # Project/workspace detection hints
│   ├── server/               # HTTP API surface
│   ├── setup/                # Deprecated compatibility stub only
│   ├── store/                # SQLite/PostgreSQL-backed storage
│   ├── sync/                 # Chunked sync/export flows
│   └── tui/                  # Local terminal browser
├── plugin/obsidian/          # Standalone external client package
├── skills/                   # Contributor guardrails and workflow skills
└── docs/                     # Product and contributor documentation
```

The only in-repo integration package preserved here is `plugin/obsidian/`, because it is a standalone client package rather than a vendor configurator owned by Lore core.

---

## Contributor Implications

- Tell the product story from the hosted/runtime surface first.
- Treat SQLite and TUI as supported local compatibility surfaces, not the default identity.
- Do not add new Lore-owned vendor setup automation.
- Prefer stable primitives (`lore serve`, `/mcp`, `lore mcp`, runtime env vars, auth/JWT, project hints) over vendor-specific packaging.
- Keep project/workspace detection hints documented as Lore-owned runtime metadata, not as external configurator logic.
