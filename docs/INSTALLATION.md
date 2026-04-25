[← Back to README](../README.md)

# Installation

- [Homebrew (macOS / Linux)](#homebrew-macos--linux)
- [Windows](#windows)
- [Install from source (macOS / Linux)](#install-from-source-macos--linux)
- [Download binary (all platforms)](#download-binary-all-platforms)
- [First run](#first-run)
- [Railway preview setup](#railway-preview-setup)
- [Environment Variables](#environment-variables)
- [Local PostgreSQL validation](#local-postgresql-validation)

---

## Homebrew (macOS / Linux)

```bash
brew install alferio94/tap/lore
```

Upgrade:

```bash
brew update && brew upgrade lore
```

---

## Windows

### Option A: `go install`

```powershell
go install github.com/alferio94/lore/cmd/lore@latest
```

### Option B: build from source

```powershell
git clone https://github.com/alferio94/lore.git
cd lore
go install ./cmd/lore
```

### Option C: download the prebuilt binary

Download the latest Windows archive from [GitHub Releases](https://github.com/alferio94/lore/releases).

> **Antivirus false positives on prebuilt binaries**
>
> Some Windows AV products flag unsigned binaries from small open-source projects. Lore's release binaries are built from public source, but if your environment is strict, prefer `go install` or a local source build.

---

## Install from source (macOS / Linux)

```bash
git clone https://github.com/alferio94/lore.git
cd lore
go install ./cmd/lore
```

---

## Download binary (all platforms)

Grab the latest release for your platform from [GitHub Releases](https://github.com/alferio94/lore/releases).

| Platform | File |
|----------|------|
| macOS (Apple Silicon) | `lore_<version>_darwin_arm64.tar.gz` |
| macOS (Intel) | `lore_<version>_darwin_amd64.tar.gz` |
| Linux (x86_64) | `lore_<version>_linux_amd64.tar.gz` |
| Linux (ARM64) | `lore_<version>_linux_arm64.tar.gz` |
| Windows (x86_64) | `lore_<version>_windows_amd64.zip` |
| Windows (ARM64) | `lore_<version>_windows_arm64.zip` |

---

## First run

### Shared/cloud runtime

Run the hosted/shared surface with:

```bash
lore serve
```

For shared deployments, set:

- `DATABASE_URL` for PostgreSQL
- `LORE_BASE_URL` for the public runtime URL
- `LORE_JWT_SECRET` for stable admin sessions/auth

### MCP stdio

If your client wants stdio MCP instead of HTTP:

```bash
lore mcp --tools=agent
```

### Local mode

If `DATABASE_URL` is unset, Lore uses SQLite automatically. You can optionally browse local data with:

```bash
lore tui
```

The TUI is a local convenience surface, not the primary hosted/admin workflow.

### Deprecated setup command

`lore setup [agent]` is still callable only as a compatibility stub. It no longer installs vendor plugins or writes agent config.

---

## Railway preview setup

Use this path only for the first hosted MCP preview on Railway. Keep the existing runtime contract: Railway builds the repo Dockerfile, starts `lore serve`, and points health checks to `GET /health`.

Required env/runtime contract:

- `DATABASE_URL` — REQUIRED. Must point to the Railway-managed PostgreSQL service.
- `LORE_ENV=staging` — REQUIRED for the hosted preview contract.
- `LORE_BASE_URL` — REQUIRED. Set this to the public Railway URL (for example `https://<your-service>.up.railway.app`).
- `LORE_JWT_SECRET` — REQUIRED. Use a persistent 32+ byte secret; staging startup fails if it is missing or shorter.
- `LORE_BOOTSTRAP_ADMIN_PASSWORD` — REQUIRED. Lore will not invent a default password in any environment.
- `LORE_BOOTSTRAP_ADMIN_EMAIL` — REQUIRED in staging. If omitted locally, Lore defaults only the email to `admin@admin.com`.
- `LORE_BOOTSTRAP_ADMIN_NAME` — optional display name for the bootstrap admin.
- `PORT` — injected by Railway.
- `LORE_PORT` — optional override; if set, it wins. Railway injects `PORT`; leave `LORE_PORT` unset unless you intentionally need to override the platform port.
- `LORE_HOST` — optional; staging already defaults to `0.0.0.0`, so only set it if you are overriding host behavior explicitly.

Set `LORE_ENV=staging`, `DATABASE_URL`, `LORE_BASE_URL`, and `LORE_JWT_SECRET` for the hosted preview contract. Also set the bootstrap admin env vars before exposing the runtime publicly. Bind/base URL behavior must stay explicit: bind on the Railway-visible host/port, but publish the external URL through `LORE_BASE_URL` so callbacks, links, and MCP clients use the public address instead of an internal bind target.

Minimal smoke steps after deploy:

```bash
curl -i "$LORE_BASE_URL/health"
```

The response should be `200 OK` only when the PostgreSQL-backed store is reachable. Then run a `/health` and `/mcp` smoke check by initializing any MCP HTTP client against:

```text
${LORE_BASE_URL}/mcp
```

The smoke is complete when the MCP client initializes successfully and can perform a minimal runtime flow with `Authorization: Bearer <jwt>`. For PostgreSQL-backed hosted validation, include skill-catalog reads such as `lore_list_skills`, `lore_get_skill`, or `skills://{name}` after seeding a test skill. Pending, disabled, or deleted users should receive `403` even if their token was minted earlier.

Excluded scope: no web view/dashboard/browser UI work, no TUI work, no agent configurators/plugins, and no production auth or multi-user hardening.

---

## Environment Variables

| Variable | Description | Default |
|---|---|---|
| `LORE_DATA_DIR` | Local SQLite data directory | `~/.lore` |
| `LORE_PORT` | Optional HTTP server port override; leave unset on Railway | unset |
| `PORT` | Cloud-host fallback port when `LORE_PORT` is unset | unset |
| `DATABASE_URL` | PostgreSQL selects shared runtime storage; required to be PostgreSQL in staging | unset |
| `LORE_PROJECT` | Override project detection for MCP | auto |
| `LORE_ENV` | `local` or `staging` | `local` |
| `LORE_HOST` | Bind host override | `127.0.0.1` local / `0.0.0.0` staging |
| `LORE_BASE_URL` | Public base URL for hosted/staging runtime | derived locally |
| `LORE_JWT_SECRET` | JWT secret for admin/auth; staging requires 32+ bytes | generated per process locally if unset |
| `LORE_BOOTSTRAP_ADMIN_EMAIL` | Bootstrap admin email; required in staging | `admin@admin.com` locally when unset |
| `LORE_BOOTSTRAP_ADMIN_PASSWORD` | Bootstrap admin password; never auto-generated | unset |
| `LORE_BOOTSTRAP_ADMIN_NAME` | Bootstrap admin display name | unset |
| `LORE_COOKIE_SECURE` | Override secure cookie behavior | env-dependent |
| `LORE_GOOGLE_CLIENT_ID` / `LORE_GOOGLE_CLIENT_SECRET` | Optional Google auth | unset |
| `LORE_GITHUB_CLIENT_ID` / `LORE_GITHUB_CLIENT_SECRET` | Optional GitHub auth | unset |

User lifecycle for this MVP:

- Registration defaults to `role=na`, `status=pending`.
- Admins can move users to `active` or `disabled` and assign only the canonical roles `admin`, `tech_lead`, `developer`, or `na`.
- OAuth follows the same lifecycle; OAuth-created users remain pending until approved.
- JWTs are checked against current store state on protected requests, but Lore does not yet ship refresh tokens, token rotation, or a dedicated revocation list.

Backend selection:

- unset `DATABASE_URL` → SQLite locally; staging startup fails
- `postgres://...` or `postgresql://...` → PostgreSQL
- other valid URLs → SQLite locally; staging startup fails
- malformed `DATABASE_URL` → startup fails before store initialization

---

## Local PostgreSQL validation

This repo includes a host-app validation path for the PostgreSQL shared-runtime surface:

```bash
docker compose -f docker-compose.postgres.yml up -d postgres
scripts/validate-postgres-local.sh
```

That path validates Lore's shared-runtime backend behavior while still running the Go app locally. The script seeds a skill directly into the PostgreSQL-backed store, then smokes `lore_list_skills`, `lore_get_skill`, and `skills://{name}` through `/mcp` so the hosted/runtime support boundary matches current PostgreSQL behavior.
