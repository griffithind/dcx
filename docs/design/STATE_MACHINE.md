# DCX State Machine

## Overview

dcx uses a state machine to track devcontainer environment lifecycle. State is determined by querying Docker labels, not local files. This enables offline-safe operations.

## States

### ABSENT
No managed containers exist for this workspace.

**Characteristics:**
- No containers with matching `env_key` label
- Clean slate for new environment

**Allowed transitions:**
- → CREATED (via `up` with `--no-start`)
- → RUNNING (via `up`)

### CREATED
Containers exist but the primary container is stopped.

**Characteristics:**
- Containers with matching `env_key` exist
- Primary container (`primary=true`) is not running
- Config hash matches current (not stale)

**Allowed transitions:**
- → RUNNING (via `start` or `up`)
- → ABSENT (via `down`)

### RUNNING
Primary container is running and ready.

**Characteristics:**
- Primary container is in "running" state
- Config hash matches current

**Allowed transitions:**
- → CREATED (via `stop`)
- → ABSENT (via `down`)

### STALE
Containers exist but configuration has changed.

**Characteristics:**
- Containers with matching `env_key` exist
- `config_hash` label differs from current computed hash

**Allowed transitions:**
- → ABSENT (via `down` or `up --recreate`)
- → RUNNING (via `up` - triggers automatic recreate)

### BROKEN
Environment is in an inconsistent state.

**Characteristics:**
- Containers with `env_key` exist but no primary found
- OR multiple containers marked as primary
- OR other inconsistencies

**Allowed transitions:**
- → ABSENT (via `down` or `up --recreate`)

## State Diagram

```
                                    ┌─────────────┐
                                    │   ABSENT    │
                                    │             │
                                    └──────┬──────┘
                                           │
                        ┌──────────────────┼──────────────────┐
                        │ up               │ up --no-start    │
                        │                  │                  │
                        ▼                  ▼                  │
                 ┌─────────────┐    ┌─────────────┐          │
           ┌─────│   RUNNING   │◄───│   CREATED   │          │
           │     │             │    │             │          │
           │     └──────┬──────┘    └──────┬──────┘          │
           │            │                  │                  │
           │            │ stop             │                  │
           │            └──────────────────┘                  │
           │                                                  │
           │ config change detected                           │
           │                                                  │
           ▼                                                  │
    ┌─────────────┐                                          │
    │    STALE    │──────────────────────────────────────────┘
    │             │         up (auto-recreate)
    └──────┬──────┘
           │
           │ down / up --recreate
           │
           ▼
    ┌─────────────┐
    │   BROKEN    │──────────────────────────────────────────┐
    │             │         down / up --recreate             │
    └─────────────┘                                          │
                                                             │
           ┌─────────────────────────────────────────────────┘
           ▼
    ┌─────────────┐
    │   ABSENT    │
    └─────────────┘
```

## Command Behavior by State

### `dcx up`

| State | Behavior |
|-------|----------|
| ABSENT | Create environment, start containers |
| CREATED | Start containers |
| RUNNING | No-op (unless `--recreate`) |
| STALE | Remove old, create new, start |
| BROKEN | Remove old, create new, start |

### `dcx start` (Offline-Safe)

| State | Behavior |
|-------|----------|
| ABSENT | Error: "run `dcx up` while online" |
| CREATED | Start containers |
| RUNNING | No-op |
| STALE | Error: "run `dcx up` to recreate" |
| BROKEN | Error: "run `dcx up --recreate`" |

### `dcx stop` (Offline-Safe)

| State | Behavior |
|-------|----------|
| ABSENT | No-op |
| CREATED | No-op |
| RUNNING | Stop containers |
| STALE | Stop containers |
| BROKEN | Stop containers (best effort) |

### `dcx down` (Offline-Safe)

| State | Behavior |
|-------|----------|
| ABSENT | No-op |
| CREATED | Remove containers |
| RUNNING | Stop and remove containers |
| STALE | Stop and remove containers |
| BROKEN | Stop and remove containers |

### `dcx exec` / `dcx shell` (Offline-Safe)

| State | Behavior |
|-------|----------|
| ABSENT | Error: "no environment found" |
| CREATED | Error: "environment not running" |
| RUNNING | Execute command |
| STALE | Execute command (warn about staleness) |
| BROKEN | Error: "environment in broken state" |

### `dcx status` (Offline-Safe)

All states: Display current state and container info.

## State Detection Algorithm

```go
func GetState(ctx, envKey) State {
    // 1. List containers with env_key label
    containers := listContainers(label: env_key=envKey)

    if len(containers) == 0 {
        return ABSENT
    }

    // 2. Find primary container
    primary := findPrimary(containers)

    if primary == nil {
        return BROKEN
    }

    // 3. Check config hash (if checking staleness)
    if currentConfigHash != primary.labels.config_hash {
        return STALE
    }

    // 4. Check if running
    if primary.running {
        return RUNNING
    }

    return CREATED
}
```

## Staleness Detection

Config hash is computed from:

1. Raw devcontainer.json content (JSONC stripped)
2. Compose file contents (for compose plans)
3. Dockerfile content (for build plans)
4. Features configuration
5. Schema version

Changes to any of these trigger STALE state.

## Recovery Procedures

### From STALE

```bash
# Option 1: Recreate environment
dcx up  # Auto-recreates

# Option 2: Force recreate
dcx up --recreate

# Option 3: Manual cleanup
dcx down
dcx up
```

### From BROKEN

```bash
# Force cleanup and recreate
dcx down
dcx up

# Or with single command
dcx up --recreate
```

## Offline Safety

Commands marked as offline-safe:
- Never pull images
- Never build images
- Never fetch features
- Only use Docker socket operations

These commands work even when:
- Network is unavailable
- Registry is unreachable
- Internet is disconnected
