<p align="center">
  <img width="1024" height="340" alt="image" src="https://github.com/user-attachments/assets/32ed8985-841d-49c3-81f7-2aabc7c7c564" />
</p>

<p align="center">
  <strong>Persistent memory for AI coding agents</strong><br>
  <em>Agent-agnostic. Single binary. Zero dependencies.</em>
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

Your AI coding agent forgets everything when the session ends. Lore gives it a brain.

A **Go binary** with SQLite + FTS5 full-text search, exposed via CLI, HTTP API, MCP server, and an interactive TUI. Works with **any agent** that supports MCP — Claude Code, OpenCode, Gemini CLI, Codex, VS Code (Copilot), Antigravity, Cursor, Windsurf, or anything else.

```
Agent (Claude Code / OpenCode / Gemini CLI / Codex / VS Code / Antigravity / ...)
    ↓ MCP stdio
Lore (single Go binary)
    ↓
SQLite + FTS5 (~/.lore/lore.db)
```

## Quick Start

### Install

```bash
brew install alferio94/tap/lore
```

Windows, Linux, and other install methods → [docs/INSTALLATION.md](docs/INSTALLATION.md)

### Setup Your Agent

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

## How It Works

```
1. Agent completes significant work (bugfix, architecture decision, etc.)
2. Agent calls mem_save → title, type, What/Why/Where/Learned
3. Lore persists to SQLite with FTS5 indexing
4. Next session: agent searches memory, gets relevant context
```

Full details on session lifecycle, topic keys, and memory hygiene → [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

## MCP Tools

| Tool | Purpose |
|------|---------|
| `mem_save` | Save observation |
| `mem_update` | Update by ID |
| `mem_delete` | Soft or hard delete |
| `mem_suggest_topic_key` | Stable key for evolving topics |
| `mem_search` | Full-text search |
| `mem_session_summary` | End-of-session save |
| `mem_context` | Recent session context |
| `mem_timeline` | Chronological drill-in |
| `mem_get_observation` | Full content by ID |
| `mem_save_prompt` | Save user prompt |
| `mem_stats` | Memory statistics |
| `mem_session_start` | Register session start |
| `mem_session_end` | Mark session complete |
| `mem_capture_passive` | Extract learnings from text output |
| `mem_merge_projects` | Merge project name variants (admin) |

Full tool reference → [docs/ARCHITECTURE.md#mcp-tools](docs/ARCHITECTURE.md#mcp-tools)

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
| `lore serve [port]` | Start HTTP API (default: 7437) |
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

**Inspired by [claude-mem](https://github.com/thedotmack/claude-mem)** — but agent-agnostic, simpler, and built different.
