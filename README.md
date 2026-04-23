<p align="center">
  <img width="1600" height="640" alt="Lore — Persistent knowledge for every agent" src="https://github.com/user-attachments/assets/f7670fec-d299-4901-b093-0776d070db9b" />
</p>

# Lore

**The agent-agnostic knowledge hub for engineering teams.**

Lore gives AI agents a shared system of record: persistent memory, reusable skills, project context, and repeatable workflows — all behind one Go binary.

Agents stay interchangeable. Team knowledge stays durable. Project state survives across sessions.

## Why Lore

Most agent setups break in predictable ways:

- memory disappears when the session ends
- conventions live in scattered local files
- handoffs between agents lose critical context
- teams repeat the same onboarding and workflow prompts

Lore solves that by centralizing the knowledge your agents should share — without tying you to a single model or coding tool.

## What Lore Provides

### Persistent memory
Store decisions, bug fixes, discoveries, prompts, and structured session summaries in SQLite with full-text search.

### Shared project context
Recover recent work, prior discussions, and evolving project state without depending on one agent's local context window.

### Team skills
Serve reusable skill documents for conventions, architecture rules, workflows, and stack-specific guidance.

### Agent-agnostic access
Use Lore from MCP-compatible agents, the CLI, HTTP integrations, and the terminal UI.

## How It Fits

```text
Claude Code / OpenCode / Codex / Cursor / Windsurf / VS Code / custom MCP clients
                                   │
                          MCP, CLI, or HTTP
                                   │
                                  Lore
                                   │
                        SQLite + FTS5 persistent store
```

Lore is the knowledge layer. Agents consume it; they are not the source of truth.

## Quick Start

### 1) Install Lore

```bash
brew install alferio94/tap/lore
```

Other install options: [docs/INSTALLATION.md](docs/INSTALLATION.md)

### 2) Connect an agent

```bash
lore setup opencode
```

Also supported: Claude Code, Gemini CLI, Codex, and other MCP-compatible tools. See [docs/AGENT-SETUP.md](docs/AGENT-SETUP.md).

### 3) Start using shared memory

```bash
lore mcp
```

Or browse locally with:

```bash
lore tui
```

## Core Capabilities

| Capability | What it enables |
| --- | --- |
| Memory tools | Save and retrieve decisions, bug fixes, discoveries, prompts, and session summaries |
| Skills tools | Load shared conventions and workflows on demand |
| Project context | Recover recent work and maintain continuity across agents and sessions |
| CLI + TUI | Inspect, search, and manage Lore directly from the terminal |
| HTTP + MCP | Integrate Lore with local and remote agent setups |
| Git sync | Move knowledge across machines with chunked sync workflows |

## Typical Workflow

1. A lead defines team skills and working rules.
2. Agents load only the relevant skills for the task.
3. Important outcomes are persisted to Lore as memory.
4. The next session or next agent resumes from shared context instead of starting blind.

## Documentation

- [Installation](docs/INSTALLATION.md)
- [Agent Setup](docs/AGENT-SETUP.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Plugins](docs/PLUGINS.md)
- [Comparison](docs/COMPARISON.md)
- [Contributing](CONTRIBUTING.md)
- [Full Docs](DOCS.md)

## Product Notes

- Single Go binary
- SQLite + FTS5 storage
- Local-first by default
- Works with MCP-compatible agents rather than one vendor-specific client

## Roadmap

- Web admin for skills and project oversight
- Stronger multi-user and deployment flows
- Deeper agent setup automation
- Continued refinement of workflows, memory quality, and team controls

## License

MIT

---

Built on top of the persistent-memory foundation from engram, adapted here for shared team knowledge and agent workflows.
