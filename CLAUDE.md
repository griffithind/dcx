# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

```bash
make build          # Build dcx with embedded agent binaries (builds agent first)
make build-agent    # Build only the dcx-agent Linux binaries (amd64/arm64)
make test           # Run unit tests (alias for test-unit)
make test-e2e       # Run end-to-end tests (requires Docker)
make test-all       # Run unit + conformance + e2e tests
make lint           # Run golangci-lint
make deadcode       # Find unused code
```

Run a single test:
```bash
go test -v -run TestShouldUpdateRemoteUserUID ./internal/build/...
make build && go test -v -tags=e2e -run TestSingleImageBasedE2E ./test/e2e/...
```

## Architecture Overview

DCX is a single-binary CLI for running devcontainers with built-in SSH support. It follows a clean layered architecture:

```
┌─────────────────────────────────────────────────────────────┐
│  CLI Layer (internal/cli/)                                  │
│  Cobra commands → CLIContext → validates state              │
└─────────────────────┬───────────────────────────────────────┘
                      │ delegates to
┌─────────────────────▼───────────────────────────────────────┐
│  Service Layer (internal/service/)                          │
│  DevContainerService orchestrates lifecycle, hooks, SSH     │
└─────────────────────┬───────────────────────────────────────┘
                      │ uses
┌─────────────────────▼───────────────────────────────────────┐
│  Runtime Layer (internal/container/)                        │
│  UnifiedRuntime handles Image/Dockerfile/Compose uniformly  │
│  Docker client wraps docker CLI                             │
└─────────────────────┬───────────────────────────────────────┘
                      │ operates on
┌─────────────────────▼───────────────────────────────────────┐
│  Domain Layer (internal/devcontainer/, state/, features/)   │
│  ResolvedDevContainer, ExecutionPlan, ContainerLabels       │
└─────────────────────────────────────────────────────────────┘
```

## Package Structure

### Entry Points
- `cmd/dcx/main.go` - Main CLI bootstrap, calls `cli.Execute()`
- `cmd/dcx-agent/main.go` - Agent binary bootstrap, calls `agent.Execute()`
- `agent-embed.go` - Embedded agent binaries via `//go:embed bin/dcx-agent-linux-*.gz`

### CLI Layer (`internal/cli/`)
| File | Purpose |
|------|---------|
| `root.go` | Root command, global flags (`--workspace`, `--config`, `--quiet`, `--verbose`) |
| `context.go` | `CLIContext` - shared init for Docker client, Service, Identifiers |
| `validate.go` | State validation helpers (`RequireRunningContainer`, `RequireExistingContainer`) |
| `exec_builder.go` | Builds docker exec commands for exec/shell/run |
| `up.go`, `down.go`, `stop.go`, `restart.go` | Lifecycle commands |
| `exec.go`, `shell.go`, `run.go` | Execution commands |
| `status.go`, `logs.go`, `plan.go` | Information commands |

**CLI Pattern:**
```go
cliCtx, err := NewCLIContext()  // Init Docker, Service, Identifiers
if err != nil { return err }
defer cliCtx.Close()            // Always cleanup
return cliCtx.Service.Up(...)   // Delegate to service
```

### Service Layer (`internal/service/`)
| File | Purpose |
|------|---------|
| `devcontainer.go` | `DevContainerService` - main orchestrator |

**Key Methods:**
- `Load()` / `LoadWithOptions()` - Resolve devcontainer configuration
- `Plan()` - Analyze state and determine action (Create/Start/Recreate/Skip)
- `Up()` - Full lifecycle: load → validate → build → create → hooks → SSH
- `QuickStart()` - Fast path when container exists and is up-to-date
- `Down()` / `DownWithIDs()` - Remove containers
- `Build()` - Build images without starting
- `Lock()` - Generate/verify lockfile

**Up() Flow:**
```
Load config → Validate host → Check state → Fetch secrets
    → Create/start container → Deploy agent → Mount secrets
    → Run lifecycle hooks → Setup SSH
```

### Runtime Layer (`internal/container/`)
| File | Purpose |
|------|---------|
| `unified.go` | `UnifiedRuntime` - strategy pattern for all plan types |
| `docker.go` | `Docker` client - singleton, wraps docker CLI |
| `compose.go` | `Compose` client - wraps docker compose CLI |
| `exec.go` | Command execution in containers |
| `secrets.go` | Secret mounting after container start |

**UnifiedRuntime Plan Dispatch:**
```go
switch plan := r.resolved.Plan.(type) {
case *devcontainer.ComposePlan:
    return r.upCompose(ctx, opts, plan)
case *devcontainer.ImagePlan, *devcontainer.DockerfilePlan:
    return r.upSingle(ctx, opts)
}
```

### Domain Layer

#### Configuration (`internal/devcontainer/`)
| File | Purpose |
|------|---------|
| `parser.go` | Parse devcontainer.json with JSONC support |
| `config.go` | `DevContainerConfig` struct with custom unmarshalers |
| `types.go` | `StringOrSlice`, `LifecycleCommand`, `Mount`, `PortSpec` |
| `substitute.go` | Variable substitution (`${localEnv:VAR}`, `${containerWorkspaceFolder}`) |
| `builder.go` | `Builder.Build()` - creates `ResolvedDevContainer` |
| `resolved.go` | `ResolvedDevContainer` - central domain object |
| `plan.go` | `ExecutionPlan` interface (sealed) with ImagePlan, DockerfilePlan, ComposePlan |
| `hashes.go` | Config/Dockerfile/Compose/Features hash computation |

#### State (`internal/state/`)
| File | Purpose |
|------|---------|
| `manager.go` | `StateManager` - detects state via Docker labels |
| `state.go` | `ContainerState` enum (Absent, Created, Running, Stopped, Stale, Broken) |
| `labels.go` | `ContainerLabels` struct, label schema (prefix: `com.griffithind.dcx`) |

#### Features (`internal/features/`)
| File | Purpose |
|------|---------|
| `manager.go` | `Manager.ResolveAll()` - entry point |
| `resolver.go` | Resolve features from local, OCI registry, or HTTP |
| `ordering.go` | Topological sort with dependency resolution |
| `dockerfile.go` | Generate Dockerfile for feature installation |

### Build Layer (`internal/build/`)
| File | Purpose |
|------|---------|
| `builder.go` | `ImageBuilder` interface |
| `dockerfile.go` | CLI-based builder using `docker buildx build` |
| `features.go` | Build derived images with features |
| `uid.go` | UID update layer for host/container user matching |

### Supporting Packages
- `internal/lifecycle/` - Hook execution (onCreateCommand, postStartCommand, etc.)
- `internal/ssh/` - SSH agent detection, host config management
- `internal/compose/` - Docker Compose override file generation
- `internal/shortcuts/` - Shortcut resolution from customizations.dcx
- `internal/agent/` - Agent CLI and SSH server implementation

## Key Types

### ResolvedDevContainer (`devcontainer/resolved.go`)
Central domain object after all resolution:
```go
type ResolvedDevContainer struct {
    ID, Name, ConfigPath, ConfigDir, LocalRoot  // Identity
    Plan ExecutionPlan                           // Image/Dockerfile/Compose
    BaseImage, ServiceName, WorkspaceFolder      // Runtime config
    RemoteUser, ContainerUser, EffectiveUser     // User config
    ContainerEnv, RemoteEnv map[string]string    // Environment
    Mounts []Mount                               // Volume mounts
    Features []*features.Feature                 // Resolved features
    Hashes *ContentHashes                        // For staleness detection
    Labels *state.ContainerLabels                // Container labels
}
```

### ExecutionPlan (`devcontainer/plan.go`)
Sealed interface with three implementations:
- `ImagePlan{Image string}` - Pre-built image
- `DockerfilePlan{Dockerfile, Context, Args, Target string}` - Custom build
- `ComposePlan{Files []string, Service, ProjectName string}` - Docker Compose

### ContainerState (`state/state.go`)
```go
StateAbsent   // No managed containers
StateCreated  // Exists but stopped
StateRunning  // Primary container running
StateStopped  // Explicitly stopped
StateStale    // Config hash changed - needs rebuild
StateBroken   // Inconsistent state
```

### ContainerLabels (`state/labels.go`)
All state persisted in Docker labels (prefix `com.griffithind.dcx`):
- `workspace.id`, `workspace.name`, `workspace.path`
- `hash.config`, `hash.dockerfile`, `hash.compose`, `hash.features`, `hash.overall`
- `build.method` (image/dockerfile/compose)
- `compose.project`, `compose.service`, `container.primary`
- `features.installed`, `features.config`

## Data Flows

### Configuration Resolution
```
devcontainer.json (JSONC)
    ↓ Parse()
DevContainerConfig
    ↓ SubstituteVariables()
DevContainerConfig (substituted)
    ↓ Builder.Build()
    ├─ Create ExecutionPlan
    ├─ features.Manager.ResolveAll() → topological sort
    ├─ Compute hashes
    └─ Merge feature config (mounts, caps, env)
    ↓
ResolvedDevContainer
```

### Container Lifecycle (Up)
```
CLI: dcx up
    ↓ NewCLIContext()
CLIContext (Docker, Service, Identifiers)
    ↓ Service.Up()
    ├─ Load() → ResolvedDevContainer
    ├─ StateManager.GetState() → check labels
    ├─ If stale: Down() first
    ├─ UnifiedRuntime.Up()
    │   ├─ ImagePlan: Pull image
    │   ├─ DockerfilePlan: docker buildx build
    │   └─ ComposePlan: docker compose up with override
    ├─ BuildWithFeatures() → derived image
    ├─ ApplyUIDUpdate() → final image
    ├─ CreateContainer() with labels
    ├─ DeployAgent() → copy dcx-agent to container
    ├─ MountRuntimeSecrets()
    ├─ RunLifecycleHooks() (onCreate, postStart, etc.)
    └─ SetupSSHAccess() → update ~/.ssh/config
```

### State Detection
```
StateManager.GetState()
    ↓ Docker.ListContainers(labels)
Find containers with workspace.id or workspace.name
    ↓ Find primary container
Compare stored hash vs current hash
    ↓
Return (ContainerState, ContainerInfo)
```

## Key Patterns

### CLIContext Pattern
All commands use shared initialization:
```go
cliCtx, err := NewCLIContext()
defer cliCtx.Close()
// cliCtx.Docker - singleton Docker client
// cliCtx.Service - DevContainerService
// cliCtx.Identifiers - project/workspace IDs
```

### Strategy Pattern (UnifiedRuntime)
Single `Up()` method dispatches to plan-specific implementation internally.

### Sealed Interface (ExecutionPlan)
```go
type ExecutionPlan interface {
    Type() PlanType
    sealed()  // Prevents external implementations
}
```
Enables exhaustive type switches - compiler ensures all cases handled.

### Label-Based State
No database - all state in Docker container labels. Survives Docker restarts, works offline.

### Image Building Pipeline
```
Base Image → Features Layer → UID Update Layer → Final Image
```
Each step tagged and cached. Skipped if already built with same hash.

## Variable Substitution

Supported patterns in `devcontainer.json`:
- `${localEnv:VAR}` / `${localEnv:VAR:default}` - Host environment
- `${containerEnv:VAR}` - Container environment
- `${localWorkspaceFolder}` - Host workspace path
- `${containerWorkspaceFolder}` - Container workspace path
- `${devcontainerId}` - Workspace ID hash
- `${userHome}` - User's home directory

## Feature Resolution

Features resolved in order:
1. Parse feature map from devcontainer.json
2. Resolve each feature (local path, OCI registry, HTTP tarball)
3. Recursively resolve dependencies (`dependsOn`, `installsAfter`)
4. Topological sort with soft dependency scoring
5. Generate Dockerfile with staged feature installation

OCI features fetched from registries like `ghcr.io/devcontainers/features/go:1`.
Lockfile support for reproducible builds via digest pinning.

## DCX Customizations

Stored in `customizations.dcx` within devcontainer.json:
```json
{
  "customizations": {
    "dcx": {
      "shortcuts": {
        "r": {"prefix": "rails", "passArgs": true},
        "rw": "bin/jobs --skip-recurring"
      }
    }
  }
}
```

## Agent Binary

The `dcx-agent` binary is embedded (gzip-compressed) and deployed to containers:
- `ssh-server` - SSH server in stdio mode for editor connections
- `ssh-agent-proxy` - TCP↔Unix socket proxy for SSH agent forwarding

Uses stdlib `flag` (no Cobra) to minimize binary size.

## Testing & Verification

**Full verification before committing:**
```bash
make build && make lint && make deadcode && make test-all
```

| Type | Location | Run Command |
|------|----------|-------------|
| Unit | `internal/*/` | `make test` |
| E2E | `test/e2e/` | `make test-e2e` (requires Docker) |
| Conformance | `test/conformance/` | Part of `make test-all` |
| Lint | - | `make lint` (golangci-lint) |
| Deadcode | - | `make deadcode` (find unused code) |

Single test:
```bash
go test -v -run TestName ./internal/package/...
make build && go test -v -tags=e2e -run TestE2EName ./test/e2e/...
```

## Important Files Quick Reference

| Purpose | Path |
|---------|------|
| CLI entry | `cmd/dcx/main.go` |
| Main service | `internal/service/devcontainer.go` |
| Runtime dispatch | `internal/container/unified.go` |
| Config parsing | `internal/devcontainer/parser.go` |
| Config types | `internal/devcontainer/config.go` |
| Resolved container | `internal/devcontainer/resolved.go` |
| State detection | `internal/state/manager.go` |
| Feature resolution | `internal/features/manager.go` |
| Image building | `internal/build/dockerfile.go` |
