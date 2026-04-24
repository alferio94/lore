[← Back to README](../README.md)

# Installation

- [Homebrew (macOS / Linux)](#homebrew-macos--linux)
- [Windows](#windows)
- [Install from source (macOS / Linux)](#install-from-source-macos--linux)
- [Download binary (all platforms)](#download-binary-all-platforms)
- [Requirements](#requirements)
- [Environment Variables](#environment-variables)
- [Windows Config Paths](#windows-config-paths)

---

## Homebrew (macOS / Linux)

```bash
brew install alferio94/tap/lore
```

Upgrade to latest:

```bash
brew update && brew upgrade lore
```

> **Migrating from Cask?** If you installed lore before v1.0.1, it was distributed as a Cask. Uninstall first, then reinstall:
> ```bash
> brew uninstall --cask lore 2>/dev/null; brew install alferio94/tap/lore
> ```

---

## Windows

**Option A: Install via `go install` (recommended for technical users)**

If you have Go installed, this is the cleanest and most trustworthy path — the binary is compiled on your machine from source, so no antivirus will flag it:

```powershell
go install github.com/alferio94/lore/cmd/lore@latest
# Binary goes to %GOPATH%\bin\lore.exe (typically %USERPROFILE%\go\bin\)
```

Ensure `%GOPATH%\bin` (or `%USERPROFILE%\go\bin`) is on your `PATH`.

**Option B: Build from source**

```powershell
git clone https://github.com/alferio94/lore.git
cd lore
go install ./cmd/lore
# Binary goes to %GOPATH%\bin\lore.exe (typically %USERPROFILE%\go\bin\)

# Optional: build with version stamp (otherwise `lore version` shows "dev")
$v = git describe --tags --always
go build -ldflags="-X main.version=local-$v" -o lore.exe ./cmd/lore
```

**Option C: Download the prebuilt binary**

1. Go to [GitHub Releases](https://github.com/alferio94/lore/releases)
2. Download `lore_<version>_windows_amd64.zip` (or `arm64` for ARM devices)
3. Extract `lore.exe` to a folder in your `PATH` (e.g. `C:\Users\<you>\bin\`)

```powershell
# Example: extract and add to PATH (PowerShell)
Expand-Archive lore_*_windows_amd64.zip -DestinationPath "$env:USERPROFILE\bin"
# Add to PATH permanently (run once):
[Environment]::SetEnvironmentVariable("Path", "$env:USERPROFILE\bin;" + [Environment]::GetEnvironmentVariable("Path", "User"), "User")
```

> **Antivirus false positives on prebuilt binaries**
>
> Windows Defender and other antivirus tools (ESET, Brave's built-in scanner) have flagged some
> lore prebuilt releases as malware (`Trojan:Script/Wacatac.H!ml` or similar). This is a
> **heuristic false positive**. The binary is built reproducibly from the public source code
> via GoReleaser and contains no malicious code.
>
> **Why does this happen?** Prebuilt binaries from small open-source projects are unsigned (code
> signing certificates cost hundreds of dollars per year). Many AV engines automatically flag
> unsigned executables from unknown publishers, especially recently compiled Go binaries. The
> same alert has been observed on Claude Code's own MSIX installer, which confirms this is an
> AV heuristic issue, not a code problem.
>
> **Maintainer stance:** We will not pay for a code signing certificate at this time. This is a
> distribution trust problem, not a security problem. The source code is fully auditable.
>
> **Recommended workaround:** Technical Windows users should prefer **Option A (`go install`)** or
> **Option B (build from source)**. Binaries you compile locally will not trigger AV alerts because
> they originate from your own machine.

> **Other Windows notes:**
> - Data is stored in `%USERPROFILE%\.lore\lore.db`
> - Override with `LORE_DATA_DIR` environment variable
> - All core features work natively: CLI, MCP server, TUI, HTTP API, Git Sync
> - No WSL required for the core binary — it's a native Windows executable

---

## Install from source (macOS / Linux)

```bash
git clone https://github.com/alferio94/lore.git
cd lore
go install ./cmd/lore

# Optional: build with version stamp (otherwise `lore version` shows "dev")
go build -ldflags="-X main.version=local-$(git describe --tags --always)" -o lore ./cmd/lore
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

## Requirements

- **Go 1.25+** to build from source (not needed if installing via Homebrew or downloading a binary)
- That's it. No runtime dependencies.

The binary includes SQLite (via [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) — pure Go, no CGO). Works natively on **macOS**, **Linux**, and **Windows** (x86_64 and ARM64).

---

## Environment Variables

| Variable | Description | Default |
|---|---|---|
| `LORE_DATA_DIR` | Data directory | `~/.lore` (Windows: `%USERPROFILE%\.lore`) |
| `LORE_PORT` | Preferred HTTP server port for `lore serve` (highest env precedence) | `7437` |
| `PORT` | Cloud-host fallback port when `LORE_PORT` is unset | unset |
| `DATABASE_URL` | `postgres://` / `postgresql://` selects PostgreSQL; other URLs keep SQLite as default | unset |

Port precedence in `lore serve`: positional argument (`lore serve 9090`) → `LORE_PORT` → `PORT` → `7437`.

Backend selection in `lore serve`:

- unset `DATABASE_URL` → SQLite
- `postgres://...` or `postgresql://...` → PostgreSQL
- any other valid URL (for example `sqlite:///tmp/lore.db`) → SQLite
- malformed `DATABASE_URL` → startup fails before store initialization

## Local PostgreSQL validation

This repo includes a host-app validation path for the first PostgreSQL slice:

```bash
docker compose -f docker-compose.postgres.yml up -d postgres
scripts/validate-postgres-local.sh
```

The script starts only PostgreSQL in Docker and runs the Go app locally against it. It verifies `/health`, session create/end, and core observation CRUD. Search parity, full app containerization, and deployment wiring remain out of scope for this change.

---

## Windows Config Paths

When using `lore setup`, config files are written to platform-appropriate locations:

| Agent | macOS / Linux | Windows |
|-------|---------------|---------|
| OpenCode | `~/.config/opencode/` | `%APPDATA%\opencode\` |
| Gemini CLI | `~/.gemini/` | `%APPDATA%\gemini\` |
| Codex | `~/.codex/` | `%APPDATA%\codex\` |
| Claude Code | Managed by `claude` CLI | Managed by `claude` CLI |
| VS Code | `.vscode/mcp.json` (workspace) or `~/Library/Application Support/Code/User/mcp.json` (user) | `.vscode\mcp.json` (workspace) or `%APPDATA%\Code\User\mcp.json` (user) |
| Antigravity | `~/.gemini/antigravity/mcp_config.json` | `%USERPROFILE%\.gemini\antigravity\mcp_config.json` |
| Data directory | `~/.lore/` | `%USERPROFILE%\.lore\` |
