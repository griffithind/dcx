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
make deadcode       # Fail if unused code is found (pinned deadcode)
```

Run a single test:
```bash
go test -v -run TestShouldUpdateRemoteUserUID ./internal/build/...
make build && go test -v -tags=e2e -run TestSingleImageBasedE2E ./test/e2e/...
```

E2E tests run commands with `dcx ssh -- …` (OpenSSH + the generated config); use `helpers.RunDCXInPTY` for interactive sessions. `make lint` includes `depguard` rules (`.golangci.yml`) for the package rules below.

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
│  Provisioner handles Image/Dockerfile/Compose uniformly     │
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
- `cmd/dcx-agent/main.go` - Agent binary bootstrap, calls `agent.Execute()` (`internal/agent`)
- `agent-embed.go` - Embeds `agentbin/` (the gzip-compressed agent binaries `make build-agent` writes there; the directory is committed so a clean checkout compiles)

### CLI Layer (`internal/cli/`)
| File | Purpose |
|------|---------|
| `root.go` | Root command, global flags (`--workspace`, `--config`, `--quiet`, `--verbose`) |
| `context.go` | Composition root: `commandContext` (Ctrl-C aware), `newService`, `CLIContext` (Docker, Service, Identity) |
| `validate.go` | State validation helpers (`RequireRunningContainer`, `RequireExistingContainer`) |
| `up.go`, `down.go`, `stop.go`, `restart.go` | Lifecycle commands |
| `ssh.go`, `run.go` | `dcx ssh [-- cmd]` (execs OpenSSH to the workspace alias) and shortcuts on top of it |
| `status.go`, `logs.go`, `list.go` | Information commands |

Commands get their context, engine and service from `context.go` (`NewCLIContext`, or `commandContext` + `newService` for commands that must work without a Docker daemon); they never construct a service or Docker client themselves.

**CLI Pattern:**
```go
cliCtx, err := NewCLIContext()  // Init Docker, Service, Identity
if err != nil { return err }
defer cliCtx.Close()            // Always cleanup
return cliCtx.Service.Up(...)   // Delegate to service
```

### Service Layer (`internal/service/`)
| File | Purpose |
|------|---------|
| `devcontainer.go` | `DevContainerService` - main orchestrator |

**Key Methods:**
- `Load()` / `LoadWithOptions()` - Resolve through `devcontainer.ResolveWorkspace` (the service only supplies image metadata via the engine)
- `Stop()` / `Restart()` - Stop (honouring `shutdownAction`), or stop and bring up again through `Up`
- `Up()` - Full lifecycle: load → validate → build → create → hooks → SSH. Also the only start path: `dcx up` on a stopped container and `dcx restart` both go through it so the agent, runtime secrets and postStart hooks are re-established.
- `Status()` / `List()` - Data for `dcx status` / `dcx list`; the CLI only renders it (golden tests in `internal/cli/testdata`)
- `Down()` / `DownWithIDs()` - Remove containers
- `Build()` - Build images without starting
- `Lock()` - Generate/verify lockfile

**Up() Flow** (`internal/service/up.go`, idempotent steps; a failed or interrupted Up resumes on the next run):
```
Load config → decideUp(state, setup complete, --recreate/--rebuild) → none | resume | start | create | recreate
    → initialize (host initializeCommand)
    → container (create / recreate / start / resume)
    → agent (agentctl.Provision: versioned install, SSH secrets, setup, launch + readiness)
    → secrets (runtime secrets on tmpfs)
    → environment (/etc/environment, userEnvProbe)
    → lifecycle hooks (onCreate/updateContent/postCreate once via markers in
      /var/lib/dcx/lifecycle, postStart on each start)
    → ssh config
```

### Runtime Layer (`internal/container/`)
| File | Purpose |
|------|---------|
| `provisioner.go` | `Provisioner` - builds image layers and creates/starts the environment for all plan types |
| `engine.go` | `Engine` interface - everything the service/runtime need from the container engine |
| `docker.go` | `Docker` - the Docker CLI implementation of `Engine` (no singleton; injected) |
| `containertest/` | `Fake` in-memory `Engine` for unit tests |
| `spec.go` | `ContainerSpec` - every runtime setting of the primary container |
| `compose.go` | `ComposeProject` - the only docker compose executor (always explicit `-f` files) |
| `compose_override.go` | `composeOverride` - renders a `ContainerSpec` as compose override YAML (yaml.v3) |
| `secrets.go` | Secret mounting after container start |

**Provisioner plan dispatch:**
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
| `resolve.go` | `ResolveWorkspace()` - the one resolution pipeline (load, image metadata via `ImageMetadataSource`, lockfile, build) |
| `builder.go` | `Builder.Build()` - the build step of `ResolveWorkspace` |
| `id.go` | `NewIdentity` / `LoadIdentity` - the only place the workspace identity is computed |
| `resolved.go` | `ResolvedDevContainer` - central domain object |
| `plan.go` | `ExecutionPlan` interface (sealed) with ImagePlan, DockerfilePlan, ComposePlan |
| `hashes.go` | Config/Dockerfile/Compose/Features hash computation |

#### State (`internal/state/`)
| File | Purpose |
|------|---------|
| `manager.go` | `StateManager` - detects state via Docker labels |
| `state.go` | `ContainerState` enum (Absent, Created, Running, Stale, Broken) and `ContainerInfo` |
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
- `internal/lifecycle/` - Hook execution; `ParseCommand` is the only lifecycle command parser; `Markers` records completed once-per-container phases
- `internal/agent/` - The dcx-agent itself (SSH server, sessions, SFTP, setup); compiled into `cmd/dcx-agent`
- `internal/agentctl/` - Host side of the agent: install/upgrade, SSH secrets, setup and launch (`Provision`)
- `internal/ssh/` - Host keys, known_hosts, authorized-key collection, `hostconfig` (~/.ssh/dcx/config), `client` (in-process SSH for lifecycle hooks only)
- `internal/compose/` - Docker Compose override file generation
- `internal/shortcuts/` - Shortcut resolution from customizations.dcx
- `internal/agentproto/` - Contract shared by host and agent (binary path, port, secret paths, `SessionConfig`)
- `internal/report/` - `Reporter` interface; library code reports progress through it and never prints
- `internal/shell/` - The only POSIX shell quoter (`Quote`, `Join`)

## Key Types

### ResolvedDevContainer (`devcontainer/resolved.go`)
Central domain object after all resolution:
```go
type ResolvedDevContainer struct {
    Identity                                     // WorkspaceID, Name, ResourceName, SSHHost
    ConfigPath, ConfigDir, LocalRoot
    Config, Local *DevContainerConfig            // merged with image metadata / as written
    Plan ExecutionPlan                           // Image/Dockerfile/Compose
    BaseImage, WorkspaceFolder                   // Runtime config
    OverrideCommand bool; HostRequirements; UserEnvProbe  // resolved; read these, not Config
    RemoteUser, ContainerUser, EffectiveUser     // User config
    ContainerEnv, RemoteEnv map[string]string    // Environment
    Mounts []Mount                               // Volume mounts
    Features []*features.Feature                 // Resolved features
    ConfigHash string                            // Staleness key (hash.config label)
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
StateCreated  // Exists but not running (created or stopped)
StateRunning  // Primary container running
StateStale    // Config hash changed - needs rebuild
StateBroken   // Inconsistent state
```

### ContainerLabels (`state/labels.go`)
All state persisted in Docker labels (prefix `com.griffithind.dcx`):
- `schema.version`, `managed`
- `workspace.id`, `workspace.name`, `workspace.path`, `config.path`
- `hash.config` (staleness key), `hash.version`, `lifecycle.markers`
- `build.method` (image/dockerfile/compose), `build.base.image`, `build.derived.image`, `features.installed`
- `compose.project`, `compose.service`, `container.primary` (plus compose's own `config_files`/`working_dir`, read only)
- `ssh.port`, `ssh.listen`, `ssh.allow`: the agent endpoint chosen at create time (`container.planSSH`); read it with `container.SSHHostPort`/`SSHListenAddr`/`RecordedSSHAccess`, never by asking Docker for the published port

Labels are immutable after create: only add one if `buildLabels` writes it.

## Data Flows

### Configuration Resolution
```
devcontainer.json (JSONC)
    ↓ Parse()
DevContainerConfig
    ↓ SubstituteConfig() — exactly once, in devcontainer.Load (also with --config)
DevContainerConfig (substituted)               — Local
    ↓ MergeMetadata(image devcontainer.metadata)  — Config (ImageMetadataSource)
    ↓ lockfile (LockfileUse)
    ↓ Builder.Build()
    ├─ NewIdentity (name, resource name / compose project, SSH alias)
    ├─ Create ExecutionPlan
    ├─ features.Manager.ResolveAll() → topological sort
    ├─ Compute hashes
    └─ Merge feature config (mounts, caps, env)
    ↓
ResolvedDevContainer
```
All of it is `devcontainer.ResolveWorkspace`; up, status, build, lock, debug and the golden tests resolve through it. Commands that must not resolve (stop, down, ssh) use `LoadIdentity`, which the golden test checks against resolution.

### Container Lifecycle (Up)
```
CLI: dcx up
    ↓ NewCLIContext()
CLIContext (Docker, Service, Identity)
    ↓ Service.Up()
    ├─ Load() → ResolvedDevContainer
    ├─ StateManager.State() → check labels
    ├─ If stale: Down() first
    ├─ Provisioner.Up()
    │   ├─ ImagePlan: Pull image
    │   ├─ DockerfilePlan: docker buildx build
    │   └─ ComposePlan: docker compose up with override
    ├─ BuildWithFeatures() → derived image
    ├─ ApplyUIDUpdate() → final image
    ├─ CreateContainer() with labels
    ├─ agentctl.Provision() → install dcx-agent, SSH secrets, launch
    ├─ MountSecretsToContainer() (runtime secrets)
    ├─ RunLifecycleHooks() (onCreate, postStart, etc.)
    └─ setupSSHAccess() → ~/.ssh/dcx/config block
```

### State Detection
```
StateManager.State()
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
// cliCtx.Docker - Docker CLI engine, passed into the service
// cliCtx.Service - DevContainerService
// cliCtx.Identity - workspace ID, resource name, SSH alias (devcontainer.Identity)
```

### Strategy Pattern (Provisioner)
Single `Up()` method dispatches to plan-specific implementation internally.

### ContainerSpec (one source for runtime settings)
`Provisioner.spec` derives a `ContainerSpec` from the resolved config; `dockerRunArgs` (single container) and `composeOverride` (compose) render it. A new runtime setting goes into `ContainerSpec` and both renderers — `TestContainerSpecParity` fails otherwise. The compose override persists at `~/.dcx/compose/<workspace-id>/docker-compose.dcx.yml`; existing compose environments are operated on with the `-f` files compose recorded on the container (`ExistingComposeProject`).

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
- Base: pulled, or built from the Dockerfile on every create (BuildKit's cache makes this cheap) as `dcx/<workspace>:base`.
- Each layer is tagged `dcx/<workspace>:<features|uid>-<hash>` where the hash covers the parent image ID plus the layer's inputs (see `Provisioner.layerImages`), and reused when the tag exists.

### Naming and staleness
- Container / default compose project name: `devcontainer.ResourceName(name, id)` (`<slug>-<id8>`). Never derive Docker names from `name` alone.
- Existing environments are always located by the `workspace.id` label and operated on by container ID / recorded compose project.
- `hash.config` is the semantic staleness key from `ComputeConfigHash`; `hash.version` marks its algorithm. Containers with an older version are treated as current.

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

The `dcx-agent` binary is embedded (gzip-compressed) and deployed to `/var/lib/dcx/bin/dcx-agent` (root-owned; never `/tmp`). It runs as root (sessions switch to the session user; without a remoteUser/containerUser that is the container's default uid); dcx first runs `dcx-agent setup` to create `/var/lib/dcx` and `/run/dcx`, the shell integration and the log. How it is reached depends on the network mode (published on the container, on the compose `service:` owner, or listening on the host port with `host` networking); `--hosts` is recorded on the container and kept until a recreate changes it. Every SSH session (dcx, OpenSSH, editors) gets `/run/dcx/session.json` (`agentproto.SessionConfig`: userEnvProbe + remoteEnv, postAttachCommand for interactive sessions), written by `up`. Host keys are created only by `up` (`EnsureHostKey`); everything else uses `LoadHostKey`. SSH host blocks live in `~/.ssh/dcx/config`, included from the top of `~/.ssh/config`.

Subcommands:
- `listen` - TCP SSH server (sessions, SFTP, agent forwarding, loopback port forwarding)
- `ping` - readiness probe used by dcx
- `setup` - root-time preparation of dirs, shell integration and log
- `sftp-server` - SFTP on stdio, re-executed under the session user's credentials

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
| Runtime dispatch | `internal/container/provisioner.go` |
| Config parsing | `internal/devcontainer/parser.go` |
| Config types | `internal/devcontainer/config.go` |
| Resolved container | `internal/devcontainer/resolved.go` |
| State detection | `internal/state/manager.go` |
| Feature resolution | `internal/features/manager.go` |
| Image building | `internal/build/dockerfile.go` |

## Package Rules

- Users connect with OpenSSH (`dcx ssh`, `ssh <alias>`); dcx's own SSH client (`internal/ssh/client`) is for lifecycle hooks only.
- Only `internal/cli` (and `internal/ui`) print. Everything else takes a `report.Reporter`; commands with `--json` output pass `report.Discard`.
- Library code never constructs a Docker client; it receives a `container.Engine` (`build` uses the narrower `build.Engine`). Only `container/docker.go` runs `docker` (depguard enforces it); add an Engine method rather than calling `os/exec`. Use `containertest.Fake` in unit tests.
- Build shell command lines with `shell.Quote`/`shell.Join`; never interpolate values into `sh -c` scripts (pass them as positional args).
- Agent paths, ports and secret locations come from `agentproto`.
- Variable substitution happens once, in `devcontainer.Load` (`NewSubstitutionContext` + `SubstituteConfig`). Don't substitute again downstream. `${containerEnv:…}` is resolved at exec time with `ResolveContainerEnvRefs`.
- Container runtime settings live in `container.ContainerSpec`, rendered by both engines; run `docker compose` only through `container.ComposeProject`.
- Parse mounts with `ParseMountString` and ports with `ParsePortSpec`; don't add new parsers.
- Resolution changes are covered by golden files in `internal/devcontainer/testdata/resolve`; review the diff of `go test ./internal/devcontainer -run TestResolveGolden -args -update`.
