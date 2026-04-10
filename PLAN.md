# Lore — Knowledge Hub for AI Agent Teams

Fork de [lore](https://github.com/alferio94/lore) orientado a equipos enterprise con multi-tenancy.

## Vision

Sistema de memoria compartida por equipo/cliente donde agentes AI (agnosticos al provider) consumen skills, convenciones, estado de proyectos y memorias compartidas via MCP.

Cada cliente (Mercedes-Benz, Home Depot, etc.) tiene su propia instancia de Lore (container aislado), funcionando como la unica fuente de verdad tecnica del equipo.

## Problema que resuelve

- Skills y convenciones fragmentados en archivos locales de cada dev
- Conocimiento que se pierde entre sesiones y entre miembros del equipo
- PMs sin visibilidad del estado real de los proyectos
- Dependencia del agente especifico (vendor lock-in)
- Devs nuevos tardan semanas en tener contexto

## Arquitectura

```
┌─────────────────────────────────────────────────────┐
│              LORE INSTANCE (per client)               │
│                                                       │
│  ┌──────────────────────────────────────────────┐    │
│  │              KNOWLEDGE LAYER                  │    │
│  │  Skills (por stack), Arquitectura, ADRs       │    │
│  └──────────────────────────────────────────────┘    │
│                                                       │
│  ┌──────────────────────────────────────────────┐    │
│  │              WORKFLOW LAYER                   │    │
│  │  Reglas de trabajo: issue-first, PR policy    │    │
│  └──────────────────────────────────────────────┘    │
│                                                       │
│  ┌──────────────────────────────────────────────┐    │
│  │              PROJECT STATE                    │    │
│  │  Estado derivado de session summaries         │    │
│  │  Memorias, decisiones, discoveries            │    │
│  └──────────────────────────────────────────────┘    │
│                                                       │
│  ┌──────────────────────────────────────────────┐    │
│  │              METRICS (auto-derived)           │    │
│  │  Issues cerrados, PRs, velocity, skills used  │    │
│  └──────────────────────────────────────────────┘    │
│                                                       │
│  Ports:                                               │
│    :7438/mcp    → MCP server (HTTP/SSE) for agents   │
│    :7438/api    → REST API                            │
│    :7438/admin  → Web Admin UI                        │
└───────────────────────┬───────────────────────────────┘
                        │ MCP (agnostic)
           ┌────────────┼────────────┐
           │            │            │
      Claude Code    Cursor      Claude Desktop
      (dev)          (dev)       (PM)
```

## Decisiones de arquitectura

### Multi-tenancy: Container por cliente
- SQLite no es para multi-tenant (single-writer)
- Enterprise requiere aislamiento total de datos
- Lifecycle independiente: cliente se va, se baja el container
- Escala natural con Kubernetes

### MCP Transport: HTTP/SSE
- engram usa stdio only, pero mcp-go v0.44.0 soporta stdio, SSE, y streamable HTTP
- Agregar HTTP es cambiar una linea en el startup
- Permite conexion remota de agentes al hub

### Tool naming: lore_*
- Renombrar mem_* → lore_* para coexistir con engram local
- Los devs pueden tener engram local (personal) + lore (equipo) simultaneamente

### Skills: read-only via MCP, write via Web Admin
- Agentes NUNCA escriben skills — solo los consumen
- Skills se crean/actualizan desde la web app con auth y roles
- Evita que un agente sobreescriba convenciones del equipo accidentalmente

### Skills scope: por stack, no por proyecto
- Cada instancia de Lore es de un cliente — no hay ambiguedad
- Un skill de Angular en el hub de Mercedes ES de Mercedes
- No se necesita campo "project" en skills

### Skills: triggers separados del content
- hub_list_skills devuelve nombre + triggers (ligero, pocos tokens)
- hub_get_skill devuelve content completo (pesado, solo cuando se necesita)
- El agente decide que skills aplican leyendo triggers, no contenido completo

### MCP Resources + Tools (dual)
- Resources: para clientes que lo soporten (Claude Desktop)
- Tools: para los que no (Claude Code, Cursor — hoy no soportan Resources)
- Mismo store por atras, dos interfaces de acceso

### Agent-agnostic
- El hub es el mismo para cualquier agente MCP-compatible
- Solo cambia el archivo de instrucciones (CLAUDE.md, .cursorrules, AGENTS.md)
- El conocimiento vive en Lore, no en el agente

## Modelo de datos

### Skills
```sql
CREATE TABLE skills (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL UNIQUE,
    display_name  TEXT NOT NULL,
    category      TEXT NOT NULL,       -- "conventions", "architecture", "workflow"
    stack         TEXT,                -- "angular", "java", NULL = cross-stack
    triggers      TEXT NOT NULL,       -- cuando aplicar este skill
    content       TEXT NOT NULL,       -- contenido markdown del skill
    version       INTEGER NOT NULL DEFAULT 1,
    created_by    TEXT NOT NULL,
    updated_by    TEXT NOT NULL,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE skill_versions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    skill_id      INTEGER REFERENCES skills(id),
    version       INTEGER NOT NULL,
    content       TEXT NOT NULL,
    triggers      TEXT NOT NULL,
    changed_by    TEXT NOT NULL,
    changed_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    change_note   TEXT
);

CREATE VIRTUAL TABLE skills_fts USING fts5(
    name, display_name, triggers, content, stack
);
```

### Users (Web Admin)
```sql
CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'viewer', -- admin, tech_lead, viewer
    avatar_url    TEXT,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Existing lore tables (inherited)
- sessions, observations, observations_fts
- user_prompts, prompts_fts
- sync_chunks

## MCP Tools

### Read/Write (memorias del equipo)
```
lore_save(title, content, type, topic_key, project, scope)
lore_search(query, type, project, scope, limit)
lore_context(project, scope)
lore_session_summary(...)
lore_get_observation(id)
lore_update(id, ...)
lore_suggest_topic_key(...)
lore_capture_passive(content)
lore_save_prompt(content)
lore_session_start(...)
lore_session_end(...)
```

### Admin (curating)
```
lore_delete(id)
lore_stats()
lore_timeline(observation_id)
lore_merge_projects(...)
```

### Skills (read-only via MCP)
```
lore_list_skills(stack?, category?)
  → [{name, display_name, triggers, stack, version}]

lore_get_skill(name)
  → {name, display_name, content, triggers, version, updated_at}
```

### MCP Resources
```
skills://{name} → contenido completo del skill
```

## Web Admin

### Features
- OAuth2 login (Google/GitHub)
- CRUD de skills con editor markdown + preview
- Historial de versiones con diff
- Dashboard de proyectos (estado derivado de session summaries)
- Metricas: skills mas consultados, memorias/semana, sessions

### Roles
| Rol | Lee skills | Escribe skills | Admin |
|-----|-----------|---------------|-------|
| viewer | Si | No | No |
| tech_lead | Si | Si | No |
| admin | Si | Si | Si |

### Embebido en el binario
- Mismo proceso Go, ruta /admin
- No requiere deploy separado

## Fases de implementacion

### Fase 0 — Fork y setup base
- Fork engram → lore
- Renombrar modulo Go, binario, imports
- Cambiar puerto default (7438), data dir (~/.lore)
- Verificar compilacion y tests

### Fase 1 — MCP over HTTP/SSE
- Agregar transporte HTTP/SSE al comando serve
- Ruta /mcp en el server existente
- CORS configurable
- Mantener stdio para uso local (lore mcp)

### Fase 2 — Renaming de tools
- mem_* → lore_*
- Actualizar profile maps, tests, docs

### Fase 3 — Skills system
- Tabla skills + skill_versions + FTS5
- Store layer: ListSkills, GetSkill, CreateSkill, UpdateSkill
- MCP tools: lore_list_skills, lore_get_skill (read-only)
- MCP Resources: skills://{name}

### Fase 4 — Web Admin
- Auth: OAuth2 + JWT + roles
- Backend API: /admin/api/skills/*, /admin/api/projects/*
- Frontend: editor de skills, dashboard de proyectos
- Embebido en el binario

### Fase 5 — Docker + deployment
- Dockerfile multi-stage
- docker-compose para desarrollo
- Kubernetes manifests para produccion
- Un deployment por cliente

### Fase 6 — Integracion con agentes y SDD
- Templates: CLAUDE.md, .cursorrules, AGENTS.md
- sdd-init lee skills del hub en vez de locales
- Deteccion automatica: LORE_URL env var
- Backward compatible: sin hub, usa skills locales

### Fase 7 — Piloto y validacion
- Docker local simulando cloud
- Claude Code como dev + Claude Desktop como PM
- 2 semanas de uso real
- Metricas: skills consultados, memorias guardadas, utilidad del dashboard

## Grafo de dependencias

```
Fase 0 (Fork)
  ├──► Fase 1 (MCP HTTP)
  │       ├──► Fase 3 (Skills) ──► Fase 4 (Web Admin)
  │       └──► Fase 5 (Docker)
  └──► Fase 2 (Rename tools) ──► Fase 3
                                      │
                          Fase 6 (Integracion) ◄── Fase 4 + Fase 5
                                      │
                          Fase 7 (Piloto)
```

## Stack tecnico

- **Backend**: Go (fork de engram)
- **DB**: SQLite + FTS5 (inherited)
- **MCP**: mcp-go v0.44.0 (HTTP/SSE + stdio)
- **Web**: TBD (HTMX + Templ o Angular SPA embebido)
- **Auth**: OAuth2 (Google/GitHub) + JWT
- **Deploy**: Docker + Kubernetes
- **Base**: github.com/alferio94/lore
