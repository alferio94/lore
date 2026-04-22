---
name: lore-skill-creator
description: >
  Guide for creating well-structured skills for Lore's cloud skill system.
  Trigger: When a tech lead asks the agent to draft a skill for the Lore platform,
  or when preparing skill content for upload via the admin web interface.
license: Apache-2.0
metadata:
  author: gentleman-programming
  version: "1.0"
  stack: Go, Lore
---

## When to Use

Use this skill when:

- A tech lead asks you to draft a new skill for Lore
- You need to produce both `content` and `compact_rules` for a skill
- You are preparing skill metadata (name, display_name, stacks, categories, triggers)
- Reviewing or improving an existing skill's compact rules

---

## Key Concepts

### Lore Skill System

Lore stores team coding skills in a cloud database. Each skill has:

| Field | Purpose | Who uses it |
|-------|---------|-------------|
| `content` | Full skill (~200-500 lines): patterns, examples, decision trees, checklists | Humans (web reference), agents needing deep context |
| `compact_rules` | Condensed rules (~30-60 lines): no examples, no explanations, just the rules | Agents via orchestrator injection |
| `stacks` | Technology tags (many-to-many) | Filtering: which skills apply to which projects |
| `categories` | Classification tags (many-to-many) | Filtering: architecture, conventions, patterns, etc. |
| `triggers` | When to load this skill | Agent skill resolution |

### Who Does What

- **Tech lead**: Creates and manages skills via the Lore admin web interface. Approves all content.
- **Agent**: Drafts skill content when asked. Produces the output. NEVER publishes directly.
- **Orchestrator**: Queries `lore_list_skills` + `lore_get_skill` at session start, injects `compact_rules` into sub-agent prompts.

---

## Content Structure (Full Skill)

The `content` field MUST follow this structure. Sections marked [REQUIRED] cannot be skipped.

```markdown
## When to Use [REQUIRED]

- Bullet points of when this skill applies
- Be specific: "Creating Angular components" not "Working with Angular"

---

## Critical Patterns [REQUIRED]

### Pattern N: Name (MANDATORY|RECOMMENDED)

Explanation of the pattern with context on WHY.

\`\`\`typescript
// Full working code example with imports
// Show the CORRECT way, not anti-patterns
\`\`\`

---

## Decision Tree [RECOMMENDED]

\`\`\`
Creating something new?
+-- Component?
|   +-- Used by 1 feature? -> features/{feature}/components/
|   +-- Used by 2+ features? -> features/shared/components/
+-- Service?
    +-- ...
\`\`\`

---

## Naming Conventions [REQUIRED]

| Type | Convention | Example |
|------|-----------|---------|
| Component | kebab-case + .component.ts | user-list.component.ts |

---

## DO NOT [REQUIRED]

- List of explicit prohibitions
- Each item: NO {thing} -- use {alternative} instead

---

## Checklists [RECOMMENDED]

### New {Artifact Type}

- [ ] Requirement 1
- [ ] Requirement 2
```

### Content Guidelines

| DO | DON'T |
|----|-------|
| Start with the most critical patterns | Add generic advice the agent already knows |
| Use tables for decision trees and conventions | Write paragraphs where a table works better |
| Keep code examples complete (with imports) | Show partial snippets without context |
| Explain the WHY behind each pattern | Just list rules without reasoning |
| Include client-specific conventions | Include framework documentation |
| Cover edge cases and gotchas | Assume the reader knows the codebase |

---

## Compact Rules Structure

The `compact_rules` field is what agents actually receive in their prompt. It MUST:

1. **Cover every section** from the full content — nothing skipped
2. **Remove all code examples** — only the rule, not the implementation
3. **Remove all explanations** — no "because", no "this is important because"
4. **Use imperative statements** — "Use OnPush" not "You should use OnPush"
5. **Group by concern** — same headers as full content but condensed
6. **Stay under 60 lines** — if it's longer, you're not condensing enough

### Compact Rules Template

```markdown
## {skill-name} (compact)

### {Section 1 Name}
- Rule as imperative statement
- Another rule
- NEVER/ALWAYS {thing}

### {Section 2 Name}
- Rule
- Rule with alternatives: {thing A} for {case}, {thing B} for {other case}

### File Placement
- {artifact} -> {path} for {condition}
- {artifact} -> {other path} for {other condition}

### Naming
- {type}: {convention} ({example})

### DO NOT
- {prohibition 1}
- {prohibition 2}
```

### Transformation Rules

| Full Content Has | Compact Rules Gets |
|------------------|--------------------|
| Code example (30 lines) | One-line rule: "Components: standalone, OnPush, input()/output() signals, inject() DI" |
| Decision tree (20 lines) | 4-5 placement rules: "1 feature -> features/{feature}/, 2+ features -> features/shared/" |
| Table with 10 rows | Condensed list of key rows only |
| Explanation paragraph | Nothing — the rule speaks for itself |
| Checklist (8 items) | Implied by the rules — omit unless critical |
| "When to Use" section | Omit entirely — triggers handle this |
| "Resources" section | Omit entirely — agents don't read docs |

---

## Metadata Rules

### name
- Lowercase, hyphens: `angular-patterns`, `nestjs-conventions`
- For client-specific: `{domain}-{concern}`: `ecommerce-api-standards`
- NEVER prefix with `lore-` — that's for Lore's own internal skills

### display_name
- Human-readable: "Angular Patterns", "NestJS Conventions"

### stacks (array of IDs)
- Select from existing catalog entries (check `GET /admin/api/stacks` first)
- If a stack doesn't exist, note it — the tech lead creates it in the admin UI
- A skill SHOULD have at least one stack

### categories (array of IDs)
- Select from existing catalog entries (check `GET /admin/api/categories` first)
- Common categories: `architecture`, `conventions`, `patterns`, `testing`, `security`
- A skill SHOULD have at least one category

### triggers
- Describe WHEN an agent should load this skill
- Be specific and action-oriented
- Format: "When {action} in {context}"
- Example: "When creating components, services, or modifying routing in Angular projects"

---

## Output Format

When drafting a skill for the tech lead, produce this output:

```
## Skill: {name}

### Metadata
- **Display Name**: {display_name}
- **Stacks**: {stack names — tech lead maps to IDs}
- **Categories**: {category names — tech lead maps to IDs}
- **Triggers**: {trigger text}

### Content
{full content following the structure above}

### Compact Rules
{condensed rules following the compact rules template}
```

The tech lead copies each section into the corresponding field in the Lore admin web interface.

---

## Validation Checklist

Before presenting the drafted skill to the tech lead:

- [ ] `content` follows the required structure (When to Use, Critical Patterns, Naming, DO NOT)
- [ ] `compact_rules` covers every section from content (nothing skipped)
- [ ] `compact_rules` has zero code examples
- [ ] `compact_rules` has zero explanatory text (no "because", no "this ensures")
- [ ] `compact_rules` is under 60 lines
- [ ] Every rule in `compact_rules` traces back to a pattern in `content`
- [ ] `triggers` are specific and action-oriented
- [ ] At least one stack and one category are suggested
- [ ] `name` is lowercase with hyphens, no `lore-` prefix
