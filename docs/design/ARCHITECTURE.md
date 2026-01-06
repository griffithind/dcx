# DCX Architecture

## Overview

dcx is a CLI for running devcontainers. It implements the devcontainer specification using the Docker Engine API and docker compose CLI, without depending on @devcontainers/cli.

## Design Principles

1. **Native Docker integration** - Uses Docker Engine API for container operations
2. **Labels as database** - No local state files; container labels are the source of truth
3. **Offline-safe operations** - `start/stop/exec` work without network access
4. **Compose CLI for orchestration** - Shell out to `docker compose` for multi-container setups

## Component Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        CLI Layer                             │
│  ┌─────┐ ┌─────┐ ┌─────┐ ┌────┐ ┌────┐ ┌────┐ ┌──────┐     │
│  │ up  │ │start│ │stop │ │exec│ │down│ │stat│ │doctor│     │
│  └──┬──┘ └──┬──┘ └──┬──┘ └──┬─┘ └──┬─┘ └──┬─┘ └──┬───┘     │
└─────┼───────┼───────┼───────┼──────┼──────┼──────┼──────────┘
      │       │       │       │      │      │      │
┌─────┼───────┼───────┼───────┼──────┼──────┼──────┼──────────┐
│     ▼       ▼       ▼       ▼      ▼      ▼      ▼          │
│  ┌─────────────────────────────────────────────────────┐    │
│  │                  State Manager                       │    │
│  │  - GetState() → ABSENT|CREATED|RUNNING|STALE|BROKEN │    │
│  │  - ComputeEnvKey()                                   │    │
│  │  - FindContainers()                                  │    │
│  └─────────────────────────────────────────────────────┘    │
│                              │                               │
│                              ▼                               │
│  ┌─────────────────────────────────────────────────────┐    │
│  │                   Docker Client                      │    │
│  │  - ListContainers(labels)                           │    │
│  │  - InspectContainer()                               │    │
│  │  - StartContainer() / StopContainer()               │    │
│  │  - Exec()                                           │    │
│  └─────────────────────────────────────────────────────┘    │
│                              │                               │
│                              ▼                               │
│  ┌─────────────────────────────────────────────────────┐    │
│  │                 Docker Engine API                    │    │
│  └─────────────────────────────────────────────────────┘    │
│                                                              │
│                     Core Layer                               │
└──────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────┐
│                    Compose Orchestration                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │ Config Parser│  │   Override   │  │   Compose    │       │
│  │              │  │  Generator   │  │    Runner    │       │
│  │ - JSONC      │  │              │  │              │       │
│  │ - Variables  │  │ - Labels     │  │ - up/down    │       │
│  │ - Validation │  │ - Mounts     │  │ - start/stop │       │
│  │ - Hash       │  │ - SSH/SEL    │  │              │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
└──────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────┐
│                    Support Services                           │
│  ┌──────────────────┐      ┌──────────────────┐             │
│  │   SSH Proxy      │      │     SELinux      │             │
│  │                  │      │                  │             │
│  │ - Socket proxy   │      │ - Mode detection │             │
│  │ - Per-connection │      │ - :Z relabeling  │             │
│  │   upstream dial  │      │                  │             │
│  └──────────────────┘      └──────────────────┘             │
└──────────────────────────────────────────────────────────────┘
```

## Package Structure

```
github.com/griffithind/dcx/
├── cmd/dcx/          # Entry point
├── internal/
│   ├── cli/          # Command implementations
│   ├── config/       # devcontainer.json parsing
│   ├── state/        # Label-based state management
│   ├── docker/       # Docker Engine API client
│   ├── compose/      # docker compose CLI wrapper
│   ├── ssh/          # SSH agent proxy
│   ├── selinux/      # SELinux detection
│   └── util/         # Shared utilities
└── pkg/jsonc/        # JSONC parser wrapper
```

## Data Flow

### `dcx up` Flow

```
1. Load devcontainer.json
2. Compute env_key from workspace path
3. Query Docker for existing containers (by label)
4. Determine current state
5. If STALE/BROKEN: remove existing containers
6. If ABSENT:
   a. Generate compose override file
   b. Start SSH agent proxy
   c. Run `docker compose up -d`
   d. Apply DCX labels
7. If CREATED: start containers
8. Run lifecycle hooks
```

### `dcx start` Flow (Offline-Safe)

```
1. Compute env_key from workspace path
2. Query Docker for existing containers
3. If RUNNING: no-op
4. If CREATED: run `docker compose start`
5. If ABSENT/STALE/BROKEN: error with instruction
```

## Key Interfaces

### State Manager

```go
type Manager interface {
    GetState(ctx, envKey) (State, *ContainerInfo, error)
    FindContainers(ctx, envKey) ([]ContainerInfo, error)
    FindPrimaryContainer(ctx, envKey) (*ContainerInfo, error)
}
```

### Docker Client

```go
type Client interface {
    ListContainers(ctx, labels) ([]Container, error)
    InspectContainer(ctx, id) (*Container, error)
    StartContainer(ctx, id) error
    StopContainer(ctx, id, timeout) error
    Exec(ctx, id, config) (exitCode, error)
}
```

### Compose Runner

```go
type Runner interface {
    Up(ctx, opts) error
    Start(ctx, opts) error
    Stop(ctx, opts) error
    Down(ctx, opts) error
}
```

## Label Schema

All labels use the `io.github.dcx.` namespace:

| Label | Purpose |
|-------|---------|
| `managed` | Marks container as dcx-managed |
| `env_key` | Stable workspace identifier |
| `config_hash` | For staleness detection |
| `plan` | "compose" or "single" |
| `primary` | Identifies main container |
| `version` | Label schema version |

## Deterministic Identity

### env_key
```
env_key = base32(sha256(realpath(workspace_root)))[0:12]
```

### compose_project
```
compose_project = "dcx_" + env_key
```

### config_hash
```
config_hash = sha256(canonical_json({
    devcontainer_json: <raw content>,
    compose_files: {<path>: <content>, ...},
    features: <features config>,
    schema_version: "1"
}))
```

## Compose Override Generation

dcx generates an override file to inject:

1. **DCX labels** - For container tracking
2. **Workspace mount** - Bind mount with working_dir
3. **SSH agent mount** - Proxy socket directory
4. **Environment variables** - containerEnv, remoteEnv, SSH_AUTH_SOCK
5. **runArgs mapping** - Capabilities, devices, security options

Example generated override:

```yaml
services:
  app:
    labels:
      io.github.dcx.managed: "true"
      io.github.dcx.env_key: "abcd1234efgh"
      io.github.dcx.config_hash: "..."
      io.github.dcx.plan: "compose"
      io.github.dcx.primary: "true"
    working_dir: /workspace
    volumes:
      - /home/user/project:/workspace:Z
      - /run/user/1000/dcx/abcd1234efgh/ssh-agent:/ssh-agent:Z
    environment:
      SSH_AUTH_SOCK: /ssh-agent/agent.sock
```

## SSH Agent Proxy

The SSH proxy enables agent forwarding that survives agent restarts:

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│  Container  │    │   Proxy     │    │  SSH Agent  │
│             │    │             │    │             │
│ SSH_AUTH_   │───▶│ agent.sock  │───▶│ (upstream)  │
│   SOCK      │    │             │    │             │
└─────────────┘    └─────────────┘    └─────────────┘
                    Per-connection
                    dial to upstream
```

Features:
- New upstream connection per client connection
- Supports agent restart without container restart
- Platform-specific socket directories
- Proper permission handling (0700 dir, 0600 socket)

## SELinux Support

On enforcing SELinux systems:

1. Detect mode via `/sys/fs/selinux/enforce`
2. Apply `:Z` suffix to bind mounts
3. Only relabel directories created by dcx

## Future Extensions

### Milestone 2: Single Container
- Direct Docker API container creation
- Image building with deterministic tags

### Milestone 3: Features (Single)
- OCI feature resolution
- Dockerfile generation with feature installs

### Milestone 4: Features (Compose)
- Derived image builds for compose services
- Image override in compose files

### Milestone 5: Parity
- Enhanced runArgs mapping
- Port forwarding
- User handling
