# Security Policy

## Supported Versions

Only the latest stable release receives security fixes.

| Version | Supported |
|---------|-----------|
| latest  | ✅        |
| older   | ❌        |

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Report security issues privately via one of these channels:

1. **GitHub Security Advisories** (preferred): [Report a vulnerability](https://github.com/alferio94/lore/security/advisories/new)
2. **Email**: Contact the maintainers directly through the GitHub profile if the advisory flow is unavailable.

## What to Include

- clear description of the vulnerability
- reproduction steps
- impact assessment
- suggested mitigations, if any

## Response Timeline

- **Acknowledgement**: within 48 hours
- **Initial assessment**: within 5 business days
- **Fix target**: within 30 days for critical/high severity, best effort otherwise
- **Disclosure**: coordinated after a fix is available

## Scope

Lore's primary deployment surface is the shared/cloud runtime, and local SQLite mode remains a supported compatibility path for local installs, tests, and development. Both are in scope for security review:

### Shared/cloud runtime

When operators run `lore serve` as a shared service, important areas include:

- auth and session handling
- JWT and cookie handling
- HTTP API input validation
- `/mcp` exposure and tool authorization
- PostgreSQL-backed multi-user or shared-runtime risks

Current auth model notes:

- Bootstrap admin setup depends on `LORE_BOOTSTRAP_ADMIN_EMAIL`, `LORE_BOOTSTRAP_ADMIN_PASSWORD`, and optional `LORE_BOOTSTRAP_ADMIN_NAME`. Lore may default the bootstrap email locally, but it never invents a password.
- Canonical roles are `admin`, `tech_lead`, `developer`, and `na`; canonical statuses are `pending`, `active`, and `disabled`.
- Self-registration and OAuth-created users enter as pending until approved by an admin.
- HTTP `/mcp` requires a bearer JWT and then re-resolves the actor from current store state, so pending/disabled/deleted users are denied even with an older token.
- MVP limitation: Lore does not yet provide refresh tokens, per-token revocation lists, or global session invalidation. Revocation today depends on checking current actor state at request time.

### Local mode

When a user runs Lore locally with SQLite, important areas include:

- local data corruption
- privilege escalation
- path traversal
- injection through CLI, MCP, or HTTP inputs
- accidental disclosure of sensitive content

## Out of Scope

- vulnerabilities that require the reporter to already fully control the operator's machine or home directory
- issues in third-party vendor configurators or packaged plugins that are not owned by this repository

## Recognition

We recognize responsible disclosures in the release notes for the version that contains the fix.
