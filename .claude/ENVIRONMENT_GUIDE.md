# ENVIRONMENT_GUIDE — Dev / Staging / Prod Discipline

> On-demand reference. Agent must identify `APP_ENV` before running any command.

## Environment Definitions

| Environment | Purpose | Characteristics |
|---|---|---|
| `development` | Local development | Debug on, verbose logs, hot reload, mock or sandbox services |
| `staging` | Pre-production validation | Production-like config, sandboxed real services, debug off |
| `production` | Live system | Debug off, minimal logs, hardened config, real secrets |

## Agent Behavior

- **development:** verbose logging allowed; dev tooling + debug ports allowed;
  seed scripts and migrations allowed.
- **staging/production:** no destructive ops or migrations without written
  confirmation; never modify live config or secrets; present a plan before
  acting; the agent should not hold production credentials at all. If it
  detects it has them, tell the user.
- **unknown environment:** treat as production for safety.

## Docker Compose Pattern (Compose v2: `docker compose`)

- `compose.yaml`: base definitions, environment-agnostic (no obsolete
  top-level `version:` key).
- `compose.override.yaml`: development overrides (hot reload, debug ports,
  volume mounts); loaded **automatically** by `docker compose up`.
- `compose.prod.yaml`: production overrides (no source mounts, restart policy,
  resource limits, no debug ports, pinned image tags); used explicitly.

```bash
# Development (override file loaded automatically)
docker compose up

# Production (explicit -f flags disable automatic override loading)
docker compose -f compose.yaml -f compose.prod.yaml up -d
```

The agent must **always ask** before running any Compose command that is not
clearly development. If the project already uses different file names,
document and follow the existing names.

## Environment Files

```text
.env.example      # committed; every variable with placeholder + comment
.env              # never committed; local development values
.env.staging      # never committed
.env.production   # never committed; ideally injected by the deployment platform
```

Development and production dependencies, debug endpoints, seed data, and admin
tools must be **absent or disabled** in production images and configuration.

> Self-update: once the real Docker setup and commands exist, replace the
> generic examples with the verified commands for this project.
