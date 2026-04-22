<p align="center">
  <img width="1024" height="340" alt="Lore — Knowledge Hub for AI Agent Teams" src="https://github.com/user-attachments/assets/30bb455f-9ea4-48b2-bf71-042dcd82321e" />
</p>

<p align="center">
  <strong>The knowledge hub for AI agent teams</strong><br>
  <em>Shared memory. Team skills. Project state. One binary.</em>
</p>

<p align="center">
  <a href="docs/INSTALLATION.md">Installation</a> &bull;
  <a href="docs/AGENT-SETUP.md">Agent Setup</a> &bull;
  <a href="docs/ARCHITECTURE.md">Architecture</a> &bull;
  <a href="docs/PLUGINS.md">Plugins</a> &bull;
  <a href="CONTRIBUTING.md">Contributing</a> &bull;
  <a href="DOCS.md">Full Docs</a>
</p>

---

> **lore** `/lɔːr/` — *noun*: the accumulated knowledge, traditions, and memory of a community or craft.

Your agents forget everything when the session ends. Your team's conventions live scattered in local files. New devs spend weeks getting up to speed. **Lore fixes all of that.**

A **Go binary** that acts as the single source of technical truth for an AI-powered team. Agents connect via MCP — over stdio locally or HTTP/SSE remotely — and access shared memory, team skills, and project state. Works with **any MCP-compatible agent**: Claude Code, OpenCode, Gemini CLI, Codex, Cursor, Windsurf, VS Code, and more.

```
Claude Code (dev)     Cursor (dev)     Claude Desktop (PM)
        │                  │                  │
        └──────────────────┴──────────────────┘
                           │ MCP (HTTP/SSE or stdio)
                    ┌──────┴──────┐
                    │    LORE     │
                    ├─────────────┤
                    │   Skills    │  ← team conventions, stack rules, ADRs
                    │   Memory    │  ← decisions, bugfixes, discoveries
                    │   Projects  │  ← derived state from session summaries
                    └──────┬──────┘
                           │
                    SQLite + FTS5
                    (~/.lore/lore.db)
```

## What Lore Does

| Layer | What it stores | Who writes |
|-------|---------------|------------|
| **Skills** | Team conventions, architecture rules, workflow standards, ADRs — per stack | Tech leads via Web Admin |
| **Memory** | Decisions, bugfixes, discoveries, session summaries — scoped per project | Agents via MCP |
| **Projects** | State derived from session summaries — what's done, what's next, velocity | Auto-derived |

Agents never see each other's work directly. They see what the team has decided is important enough to persist. **The knowledge lives in Lore, not in the agent.**

## Quick Start

### Install

```bash
brew install alferio94/tap/lore
```

Windows, Linux, and other install methods → [docs/INSTALLATION.md](docs/INSTALLATION.md)

### Connect Your Agent

| Agent | One-liner |
|-------|-----------|
| Claude Code | `claude plugin marketplace add alferio94/lore && claude plugin install lore` |
| OpenCode | `lore setup opencode` |
| Gemini CLI | `lore setup gemini-cli` |
| Codex | `lore setup codex` |
| VS Code | `code --add-mcp '{"name":"lore","command":"lore","args":["mcp"]}'` |
| Cursor / Windsurf / Any MCP | See [docs/AGENT-SETUP.md](docs/AGENT-SETUP.md) |

Full per-agent config, Memory Protocol, and compaction survival → [docs/AGENT-SETUP.md](docs/AGENT-SETUP.md)

That's it. No Node.js, no Python, no Docker. **One binary, one SQLite file.**

## Docker Staging: SQLite WAL Persistence (Critical)

When running Lore in containers, `LORE_DATA_DIR` is the SQLite data directory and **must** be mounted to a persistent volume.

Lore uses SQLite WAL mode, which writes three files in the same directory:

- `lore.db`
- `lore.db-wal`
- `lore.db-shm`

All three files must live in the **same persistent volume path** under `LORE_DATA_DIR`. If only `lore.db` is persisted (or sidecar files are lost between restarts/recreates), you can see apparent data loss or inconsistent state after container restart.

`docker-compose.staging.yml` already handles this correctly with the named volume mount `lore-data:/data` (and `LORE_DATA_DIR=/data`).

## How It Works

```
1. Tech lead writes a skill (Angular conventions, API standards, PR policy)
2. Agent calls lore_list_skills → gets name + triggers (lightweight)
3. Agent calls lore_get_skill(name) → gets full content when needed
4. Agent does significant work (bugfix, decision, discovery)
5. Agent calls lore_save → persisted with FTS5 indexing
6. Next session, next agent: lore_search / lore_context → instant context
```

Full details on session lifecycle, topic keys, and memory hygiene → [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

## MCP Tools

### Memory (read/write)

| Tool | Purpose |
|------|---------|
| `lore_save` | Save observation (decision, bugfix, discovery, etc.) |
| `lore_update` | Update by ID |
| `lore_search` | Full-text search across all observations |
| `lore_context` | Recent session context for a project |
| `lore_session_summary` | End-of-session structured save |
| `lore_get_observation` | Full content by ID (untruncated) |
| `lore_suggest_topic_key` | Stable key for evolving topics |
| `lore_save_prompt` | Save user prompt |
| `lore_session_start` | Register session start |
| `lore_session_end` | Mark session complete |
| `lore_capture_passive` | Extract learnings from text output |
| `lore_timeline` | Chronological drill-in |
| `lore_stats` | Memory statistics |

### Skills (read-only via MCP)

| Tool | Purpose |
|------|---------|
| `lore_list_skills` | List skills with triggers — lightweight, low tokens |
| `lore_get_skill` | Full skill content by name |

Skills are **read-only via MCP**. Agents consume them, never write them. Skills are created and managed by tech leads through the Web Admin.

Full tool reference → [docs/ARCHITECTURE.md#mcp-tools](docs/ARCHITECTURE.md#mcp-tools)

## Skills System

Lore serves team knowledge as **skills** — structured markdown documents that agents load on demand based on triggers.

```
Agent: "I'm about to write an Angular component"
  → lore_list_skills(stack: "angular")
  → [{name: "angular-conventions", triggers: "When writing Angular components..."}]
  → lore_get_skill("angular-conventions")
  → Full conventions doc injected into context
```

Skills have version history, FTS5 search, and are managed through the Web Admin (coming in Phase 4). → [docs/ARCHITECTURE.md#skills](docs/ARCHITECTURE.md#skills)

## Terminal UI

```bash
lore tui
```

<p align="center">
<img src="assets/tui-dashboard.png" alt="TUI Dashboard" width="400" />
  <img width="400" alt="image" src="https://github.com/user-attachments/assets/0308991a-58bb-4ad8-9aa2-201c059f8b64" />
  <img src="assets/tui-detail.png" alt="TUI Observation Detail" width="400" />
  <img src="assets/tui-search.png" alt="TUI Search Results" width="400" />
</p>

**Navigation**: `j/k` vim keys, `Enter` to drill in, `/` to search, `Esc` back. Catppuccin Mocha theme.

## Git Sync

Share memories across machines. Uses compressed chunks — no merge conflicts, no huge files.

```bash
lore sync                    # Export new memories as compressed chunk
git add .lore/ && git commit -m "sync lore memories"
lore sync --import           # On another machine: import new chunks
lore sync --status           # Check sync status
```

Full sync documentation → [DOCS.md](DOCS.md)

## CLI Reference

| Command | Description |
|---------|-------------|
| `lore setup [agent]` | Install agent integration |
| `lore serve [port]` | Start HTTP API + MCP over HTTP/SSE (default: 7438) |
| `lore mcp` | Start MCP server (stdio). Accepts `--project` or `LORE_PROJECT` |
| `lore tui` | Launch terminal UI |
| `lore search <query>` | Search memories |
| `lore save <title> <msg>` | Save a memory |
| `lore timeline <obs_id>` | Chronological context |
| `lore context [project]` | Recent session context |
| `lore stats` | Memory statistics |
| `lore export [file]` | Export to JSON |
| `lore import <file>` | Import from JSON |
| `lore sync` | Git sync export |
| `lore projects list` | Show all projects with observation/session/prompt counts |
| `lore projects consolidate` | Interactive merge of similar project names (`--all`, `--dry-run`) |
| `lore projects prune` | Remove projects with 0 observations (`--dry-run`) |
| `lore version` | Show version |

## Roadmap

| Phase | Status | What |
|-------|--------|------|
| 0 — Fork & rebrand | ✅ Done | Full rebrand from engram → lore, Go module, binary, plugins |
| 1 — MCP over HTTP/SSE | ✅ Done | Remote agents connect via HTTP — foundation for team-hub mode |
| 2 — Tool rename | ✅ Done | `mem_*` → `lore_*` to coexist with local engram |
| 3 — Skills system | ✅ Done | Skills tables, store CRUD, FTS5, MCP tools + resources |
| 4 — Web Admin | 🔄 In Progress | OAuth2 + JWT + RBAC, skills editor, project dashboard |
| 5 — Docker + deployment | 🔲 Planned | Multi-stage Dockerfile, Kubernetes manifests, one instance per client |
| 6 — Agent integration | 🔲 Planned | CLAUDE.md / AGENTS.md templates, `sdd-init` reads skills from hub |
| 7 — Pilot | 🔲 Planned | Real-world validation: 2 agents, 2 roles, 2 weeks |

## Documentation

| Doc | Description |
|-----|-------------|
| [Installation](docs/INSTALLATION.md) | All install methods + platform support |
| [Agent Setup](docs/AGENT-SETUP.md) | Per-agent configuration + Memory Protocol |
| [Architecture](docs/ARCHITECTURE.md) | How it works + MCP tools + project structure |
| [Plugins](docs/PLUGINS.md) | OpenCode & Claude Code plugin details |
| [Comparison](docs/COMPARISON.md) | Why Lore vs alternatives |
| [Contributing](CONTRIBUTING.md) | Contribution workflow + standards |
| [Full Docs](DOCS.md) | Complete technical reference |

## License

MIT

---

Built on top of [engram](https://github.com/alferio94/lore) — the personal persistent memory layer that powers Lore's core. Inspired by [claude-mem](https://github.com/thedotmack/claude-mem).
