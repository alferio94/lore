---
name: lore-architecture-guardrails
description: >
  Architecture guardrails for Lore across cloud/runtime, local compatibility,
  dashboard, and integrations. Trigger: Any change that affects system boundaries, ownership,
  state flow, or cross-package responsibilities.
license: Apache-2.0
metadata:
  author: gentleman-programming
  version: "1.0"
---

## When to Use

Use this skill when:
- Adding a new subsystem or major package
- Moving responsibilities between cloud/runtime, local compatibility surfaces, dashboard, or integrations
- Changing sync flow, source-of-truth rules, or persistence boundaries

---

## Core Guardrails

1. Cloud/runtime surfaces are the primary product boundary; local SQLite/TUI are compatibility surfaces.
2. Keep external configurators and adapters thin; stable behavior belongs in Lore's Go runtime.
3. Prefer explicit boundaries: store, server, admin, MCP, sync, and local-only TUI.
4. New features must fit Lore's hosted/runtime contract before expanding local-only UX.
5. Do not hide cross-system coupling inside helpers, templates, or vendor-specific wrappers.

---

## Decision Rules

- Storage/backend concern -> `internal/store`
- HTTP or runtime enforcement -> `internal/server`
- Browser-admin/auth concern -> `internal/admin`
- MCP contract/tooling -> `internal/mcp`
- Local-only browsing concern -> `internal/tui`

---

## Validation

- Add regression tests or focused static checks for every boundary change.
- Verify docs, runtime surfaces, and local compatibility surfaces still tell the same cloud-first story.
- If the change touches sync, test both push and pull paths.
