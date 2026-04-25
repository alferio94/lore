<p align="center">
  <img width="1600" height="640" alt="Lore — Persistent knowledge for every agent" src="https://github.com/user-attachments/assets/f7670fec-d299-4901-b093-0776d070db9b" />
</p>

# Lore

**Cloud-ready memory, MCP, and admin surfaces for agent teams.**

Lore is a single Go binary that gives agents shared memory, reusable skills, project context, MCP access, and browser-based administration.

Hosted/runtime workflows come first. SQLite and the terminal UI still exist for local development, demos, tests, and compatibility workflows.

## What Lore Provides

- **Runtime APIs first** — `lore serve` exposes HTTP APIs, `/mcp`, and the admin/browser surface.
- **MCP for any agent** — `lore mcp` runs stdio MCP without vendor lock-in.
- **Persistent memory** — decisions, bugs, prompts, and session summaries live in Lore storage.
- **Shared project context** — agents resume from durable state instead of fragile local context windows.
- **Local compatibility mode** — SQLite and `lore tui` remain available for local inspection and dev workflows.

## Product Surfaces

```text
Agent clients / external configurators / browser admins
                    │
          HTTP API, /mcp, MCP stdio
                    │
                   Lore
                    │
    PostgreSQL for shared/cloud runtime or SQLite for local mode
```

Lore owns the runtime contract. Vendor-specific setup, plugin packaging, and configurator UX belong outside this repo.

## Quick Start

### 1) Install Lore

```bash
brew install alferio94/tap/lore
```

Other install options: [docs/INSTALLATION.md](docs/INSTALLATION.md)

### 2) Start the runtime

```bash
lore serve
```

This starts Lore's HTTP surface, browser-admin surface, and `/mcp` endpoint.

### 3) Connect your agent

Use Lore through either of these stable integration primitives:

```bash
lore mcp --tools=agent
```

or an HTTP client pointed at your Lore base URL.

See [docs/AGENT-SETUP.md](docs/AGENT-SETUP.md) for the supported integration contract.

### 4) Optional local browsing

```bash
lore tui
```

The TUI is a local SQLite/dev convenience surface, not the primary onboarding flow.

## Core Capabilities

| Capability | What it enables |
| --- | --- |
| HTTP + admin | Run Lore as a browser-accessible service for operators and teams |
| MCP | Connect any MCP-compatible agent over stdio or `/mcp` |
| Memory tools | Save and retrieve decisions, bug fixes, discoveries, prompts, and session summaries |
| Skills tools | Load shared conventions and workflows on demand |
| Project context | Recover recent work and maintain continuity across agents and sessions |
| Local mode | Inspect or demo Lore with SQLite and the terminal UI |
| Git sync | Move knowledge across machines with chunked sync workflows |

## Deployment Modes

- **Shared/cloud runtime**: set `DATABASE_URL` to PostgreSQL and run `lore serve` behind your preferred host.
- **Local mode**: leave `DATABASE_URL` unset and Lore uses SQLite in `~/.lore`.

Important runtime env vars:

- `DATABASE_URL`
- `LORE_BASE_URL`
- `LORE_JWT_SECRET`
- `LORE_BOOTSTRAP_ADMIN_EMAIL`
- `LORE_BOOTSTRAP_ADMIN_PASSWORD`
- `LORE_BOOTSTRAP_ADMIN_NAME`
- `LORE_PORT` / `PORT`
- `LORE_PROJECT`

See [DOCS.md](DOCS.md) and [docs/INSTALLATION.md](docs/INSTALLATION.md) for details.

### Railway preview runtime

The first Railway preview keeps Lore on the existing hosted contract: deploy `lore serve`, back it with Railway PostgreSQL through `DATABASE_URL`, and configure `PORT`, `DATABASE_URL`, `LORE_ENV`, `LORE_BASE_URL`, and `LORE_JWT_SECRET` before exposing the service publicly.

Use `LORE_ENV=staging` for Railway. Railway injects `PORT`; leave `LORE_PORT` unset unless you intentionally need to override the platform port. `DATABASE_URL` must be PostgreSQL, and `LORE_JWT_SECRET` must be persistent and at least 32 bytes. `LORE_BASE_URL` must be the public Railway URL so admin links and callbacks resolve correctly. `LORE_BOOTSTRAP_ADMIN_EMAIL` and `LORE_BOOTSTRAP_ADMIN_PASSWORD` are also required in staging; Lore may default the bootstrap email locally to `admin@admin.com`, but it never invents a default password.

After deploy, run a `/health` and `/mcp` smoke check: `curl "$LORE_BASE_URL/health"` should return `200` only when the PostgreSQL-backed store is reachable, and an MCP client should initialize successfully against `"$LORE_BASE_URL/mcp"` with `Authorization: Bearer <jwt>`. `/mcp` always re-resolves the actor from the store, so pending, disabled, or deleted users are denied even if they still hold an older token. For PostgreSQL-backed hosted/runtime validation, the minimum MCP smoke now includes `lore_list_skills` / `lore_get_skill` for active `developer` users and the admin-only tools for `admin` users.

Auth lifecycle summary for this MVP:

- Self-registration creates `role=na`, `status=pending`.
- Canonical roles are `admin`, `tech_lead`, `developer`, and `na`.
- Canonical statuses are `pending`, `active`, and `disabled`.
- OAuth follows the same approval gate: OAuth-created users stay pending until an admin activates them.
- JWTs are bearer/session transport tokens, not revocable capability tokens. Revocation is enforced by current store state on each protected request, but this MVP does not yet provide refresh tokens, logout-all-sessions, or per-token revocation lists.

No web view/dashboard/browser UI expansion, TUI changes, agent configurators/plugins, or production auth/multi-user hardening are included in this preview.

## Deprecated Setup Note

`lore setup [agent]` is retained only as a compatibility stub. It does **not** install vendor assets or write agent config. Use your external configurator or manual MCP/HTTP wiring instead.

## Documentation

- [Full Docs](DOCS.md)
- [Installation](docs/INSTALLATION.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Agent Integration Primitives](docs/AGENT-SETUP.md)
- [Integration Ownership](docs/PLUGINS.md)
- [Comparison](docs/COMPARISON.md)
- [Security](SECURITY.md)
- [Contributing](CONTRIBUTING.md)

## License

MIT

---

Built on top of the persistent-memory foundation from engram, adapted here for shared team knowledge and agent workflows.
