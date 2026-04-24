[← Back to README](../README.md)

# Installation

- [Homebrew (macOS / Linux)](#homebrew-macos--linux)
- [Windows](#windows)
- [Install from source (macOS / Linux)](#install-from-source-macos--linux)
- [Download binary (all platforms)](#download-binary-all-platforms)
- [First run](#first-run)
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

## Environment Variables

| Variable | Description | Default |
|---|---|---|
| `LORE_DATA_DIR` | Local SQLite data directory | `~/.lore` |
| `LORE_PORT` | Preferred HTTP server port | `7437` |
| `PORT` | Cloud-host fallback port when `LORE_PORT` is unset | unset |
| `DATABASE_URL` | PostgreSQL selects shared runtime storage; other cases keep SQLite active | unset |
| `LORE_PROJECT` | Override project detection for MCP | auto |
| `LORE_ENV` | `local` or `staging` | `local` |
| `LORE_HOST` | Bind host override | `127.0.0.1` local / `0.0.0.0` staging |
| `LORE_BASE_URL` | Public base URL for hosted/staging runtime | derived locally |
| `LORE_JWT_SECRET` | JWT secret for admin/auth | generated per process locally if unset |
| `LORE_COOKIE_SECURE` | Override secure cookie behavior | env-dependent |
| `LORE_GOOGLE_CLIENT_ID` / `LORE_GOOGLE_CLIENT_SECRET` | Optional Google auth | unset |
| `LORE_GITHUB_CLIENT_ID` / `LORE_GITHUB_CLIENT_SECRET` | Optional GitHub auth | unset |

Backend selection:

- unset `DATABASE_URL` → SQLite
- `postgres://...` or `postgresql://...` → PostgreSQL
- other valid URLs → SQLite
- malformed `DATABASE_URL` → startup fails before store initialization

---

## Local PostgreSQL validation

This repo includes a host-app validation path for the first PostgreSQL slice:

```bash
docker compose -f docker-compose.postgres.yml up -d postgres
scripts/validate-postgres-local.sh
```

That path validates Lore's shared-runtime backend behavior while still running the Go app locally.
