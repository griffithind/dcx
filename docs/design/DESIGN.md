# DCX Design Overview

## Architecture

dcx is a CLI for running devcontainers using the Docker Engine API and docker compose CLI, without depending on @devcontainers/cli.

### Design Principles

1. **Native Docker integration** - Uses Docker Engine API for container operations
2. **Labels as database** - No local state files; container labels are the source of truth
3. **Offline-safe operations** - `stop/exec/shell` work without network access
4. **Compose CLI for orchestration** - Shell out to `docker compose` for multi-container setups

### Package Structure

```
github.com/griffithind/dcx/
├── cmd/dcx/          # Entry point
├── internal/
│   ├── cli/          # Command implementations
│   ├── config/       # devcontainer.json parsing
│   ├── state/        # Label-based state management
│   ├── docker/       # Docker Engine API client
│   ├── runner/       # docker compose CLI wrapper
│   ├── features/     # Feature resolution and installation
│   ├── ssh/          # SSH agent proxy
│   ├── selinux/      # SELinux detection
│   └── output/       # Output formatting
└── ...
```

### Label Schema

All labels use the `io.github.dcx.` namespace:

| Label | Purpose |
|-------|---------|
| `managed` | Marks container as dcx-managed |
| `env_key` | Stable workspace identifier |
| `config_hash` | For staleness detection |
| `plan` | "compose" or "single" |
| `primary` | Identifies main container |

### Deterministic Identity

```
env_key = base32(sha256(realpath(workspace_root)))[0:12]
compose_project = "dcx_" + env_key
```

---

## State Machine

dcx uses a state machine to track devcontainer environment lifecycle. State is determined by querying Docker labels, not local files.

### States

| State | Description |
|-------|-------------|
| **ABSENT** | No managed containers exist |
| **CREATED** | Containers exist but stopped |
| **RUNNING** | Primary container is running |
| **STALE** | Configuration has changed since last build |
| **BROKEN** | Inconsistent state (no primary, multiple primaries, etc.) |

### State Transitions

```
         ┌─────────┐
         │ ABSENT  │
         └────┬────┘
              │ up
              ▼
         ┌─────────┐     stop     ┌─────────┐
         │ RUNNING │◄────────────►│ CREATED │
         └────┬────┘              └────┬────┘
              │                        │
              │ config change          │ down
              ▼                        ▼
         ┌─────────┐              ┌─────────┐
         │  STALE  │─────────────►│ ABSENT  │
         └─────────┘  up/down     └─────────┘
```

### Command Behavior

| Command | ABSENT | CREATED | RUNNING | STALE | BROKEN |
|---------|--------|---------|---------|-------|--------|
| `up` | Create & start | Start | No-op | Recreate | Recreate |
| `stop` | No-op | No-op | Stop | Stop | Best effort |
| `restart` | Error | Start | Stop & start | Error | Error |
| `down` | No-op | Remove | Stop & remove | Stop & remove | Stop & remove |
| `exec` | Error | Error | Execute | Execute (warn) | Error |

### Offline Safety

Offline-safe commands never pull images, build, or fetch features. They only use Docker socket operations and work when network is unavailable.

---

## Features System

Devcontainer Features are self-contained units of installation code that add tools and capabilities to containers.

### Feature Reference Types

| Type | Format | Example |
|------|--------|---------|
| **OCI** | `[registry/]repo/feature[:version]` | `ghcr.io/devcontainers/features/go:1` |
| **Local** | `./path/to/feature` | `./my-features/custom-tool` |
| **HTTP** | `https://example.com/feature.tar.gz` | Direct tarball URL |

### Feature Structure

```
feature/
├── devcontainer-feature.json   # Metadata and options
├── install.sh                  # Installation script
└── (other files)               # Additional resources
```

### Installation Process

1. Resolve all feature references (OCI, local, HTTP)
2. Order features by dependencies (`dependsOn`, `installsAfter`)
3. Generate Dockerfile with feature installation steps
4. Build derived image: `dcx-derived/<env_key>:<hash>`
5. Use derived image for container

### Dependency Resolution

- **Hard dependencies** (`dependsOn`): Must be present and installed first
- **Soft dependencies** (`installsAfter`): Prefer to install after these
- Cycle detection using Kahn's algorithm for topological sort

### Caching

Features cached in `~/.cache/dcx/features/<cache-key>/` with:
- OCI: SHA256 of canonical reference
- HTTP: SHA256 of URL
- Local: No caching (used directly)

---

## SSH Agent Forwarding

dcx uses a TCP-based proxy for SSH agent forwarding:

1. Host-side: TCP listener on `127.0.0.1:<port>` connected to local SSH agent
2. Container-side: dcx binary creates Unix socket, forwards to host via TCP
3. User commands use `SSH_AUTH_SOCK=/tmp/ssh-agent-<uid>.sock`

This works across all platforms (Docker Desktop, native Linux, Colima, Podman) without socket mounting issues.

---

## SELinux Support

On enforcing SELinux systems, dcx:
1. Detects mode via `/sys/fs/selinux/enforce`
2. Applies `:Z` suffix to bind mounts
3. Ensures proper container access to mounted directories
