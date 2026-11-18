# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

```bash
make build          # Build dcx with embedded agent binaries (builds agent first)
make build-agent    # Build only the dcx-agent Linux binaries (amd64/arm64)
make test           # Run unit tests (alias for test-unit)
make test-e2e       # Run end-to-end tests (requires Docker)
make test-all       # Run unit + conformance + e2e tests
make lint           # Run golangci-lint (includes gofmt/goimports and e2e-tagged files)
make fmt            # Apply the gofmt/goimports formatters
make deadcode       # Fail if unused code is found (pinned deadcode)
```

Run a single test:
```bash
go test -v -run TestShouldUpdateRemoteUserUID ./internal/build/...
make build && go test -v -tags=e2e -run TestSingleImageBasedE2E ./test/e2e/...
```

E2E tests run commands with `dcx ssh -- …` (OpenSSH + the generated config); use `helpers.RunDCXInPTY` for interactive sessions (killed after two minutes). The e2e `TestMain` calls `helpers.Isolate`: `DCX_HOME` and `DCX_SSH_DIR` point at temporary directories and `SSH_AUTH_SOCK` at an in-process agent holding a generated key (`helpers.AgentKeyFingerprint`), so a run never touches the developer's `~/.dcx`, `~/.ssh` or agent. `helpers.CreateTempWorkspace`, `CreateTempComposeWorkspace` and `CreateWorkspaceFiles` run `dcx down` when the test ends. Use pinned image tags (`alpine:3.24`). `make lint` includes `depguard` rules (`.golangci.yml`) for the package rules below.

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
| `context.go` | Composition root: `commandContext` (Ctrl-C aware), `newEngine`/`newDaemonEngine` (the only engine construction; tests swap in `containertest.Fake`; `-v` traces docker commands), `newService`, `CLIContext` (Docker, Service, Identity) |
| `validate.go` | State validation helpers (`RequireRunningContainer`, `RequireExistingContainer`) |
| `up.go`, `down.go`, `stop.go`, `restart.go` | Lifecycle commands |
| `ssh.go`, `run.go` | `dcx ssh [-- cmd]` (execs OpenSSH to the workspace alias) and shortcuts on top of it |
| `status.go`, `logs.go`, `list.go` | Information commands (`status` also shows the SSH endpoint and host key) |
| `doctor.go` | System, config and (for a running workspace) SSH agent checks |
| `upgrade.go` | Self-update from GitHub releases, verified against `checksums.txt`; semver comparison, `--force` |
| `output.go` | `printJSON`; with `--json`, `Execute` prints errors as `{"error": …}` |

Commands get their context, engine and service from `context.go` (`NewCLIContext`, or `commandContext` + `newEngine()` + `newService` for commands that must work without a Docker daemon); they never construct a service or Docker client themselves, so command-level tests run against `containertest.Fake` (`internal/cli/commands_engine_test.go`).

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
| `devcontainer.go` | `DevContainerService` - main orchestrator (Load, Down, Build, Lock) |
| `up.go` | `Up`: `decideUp`, `announceUp`, the idempotent steps |
| `status.go` | `Status` (state, drift, SSH endpoint) and `List` |
| `stop.go` | `Stop` / `Restart` |
| `lock_workspace.go` | `lockWorkspace` (`$DCX_HOME/workspaces/<id>/lock`), `tryLockWorkspace` |
| `clean.go` | `dcx clean`: unused images, and `PruneWorkspaces` (`~/.dcx` folders and older-layout files of workspaces without a container) |

**Key Methods:**
- `Load()` / `LoadWithOptions()` - Resolve through `devcontainer.ResolveWorkspace` (the service only supplies image metadata via the engine)
- `Stop()` / `Restart()` - Stop (honouring `shutdownAction`), or stop and bring up again through `Up`
- `Up()` - Full lifecycle: load → validate → prepare (build/pull) → create → hooks → SSH. With `--pull`, resolution pulls the base image first (`LoadOptions.RefreshMetadata`), so its metadata is the one that runs; `Build()` is Prepare alone, with the same secrets and flags. Also the only start path: `dcx up` on a stopped container and `dcx restart` both go through it so the agent, runtime secrets and postStart hooks are re-established.
- `Status()` / `List()` - Data for `dcx status` / `dcx list`; the CLI only renders it (golden tests in `internal/cli/testdata`)
- `Down()` / `DownWithIDs()` - Remove containers
- `Build()` - Build images without starting
- `Lock()` - Generate/verify lockfile

**Up() Flow** (`internal/service/up.go`, idempotent steps; a failed or interrupted Up resumes on the next run):
```
initialize (host initializeCommand, read from devcontainer.json alone, before resolution)
    → load config → decideUp(state, setup complete, --recreate/--rebuild) → none | resume | start | create | recreate
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
| `compose.go` | `ComposeProject` - the only docker compose executor (always explicit `-f` files); `Start` is `up -d --no-recreate` |
| `compose_override.go` | `composeOverride` - renders a `ContainerSpec` as compose override YAML (yaml.v3) |
| `secrets.go` | Secret mounting after container start |
| `entrypoint.go` | Entrypoint chaining (image metadata, then features) and the keep-alive command |
| `ssh_endpoint.go` | `planSSH` (agent port, listen address, network mode) and the recorded-endpoint readers |
| `runtime.go` | `UpOptions`, `SSHAccess` (`--hosts`), `DownOptions` |

**Provisioner plan dispatch** (`Create`, from what `Prepare` returned):
```go
switch plan := r.resolved.Plan.(type) {
case *devcontainer.ComposePlan:
    return r.createCompose(ctx, prepared.Image, access, plan)
case *devcontainer.ImagePlan, *devcontainer.DockerfilePlan:
    return r.createSingle(ctx, prepared.Image, access)
}
```

### Domain Layer

#### Configuration (`internal/devcontainer/`)
| File | Purpose |
|------|---------|
| `parser.go` | Parse devcontainer.json with JSONC support |
| `config.go` | `DevContainerConfig` struct with custom unmarshalers |
| `types.go` | `StringOrSlice` |
| `hooks.go` | `Hooks` - resolved per-phase hook lists (image metadata, then features, then devcontainer.json); all run before `up` returns (`waitFor` has no effect) |
| `baseimage.go` | `metadataImage` - the image whose devcontainer.metadata applies (parses Dockerfile `FROM`; for compose reads the compose model) |
| `compose_config.go` | `ComposeConfig`, the compose model (`docker compose config --format json` via `ComposeConfigSource`): metadata discovery, the hashes' Dockerfiles and provisioning all read it |
| `ports.go` | `PortList`/`Port` (forwardPorts, appPort), `ParsePortSpec` (`ParsePortSpecs` expands ranges), `PortForward.Collides` |
| `workspace.go` | The workspace layout: the git root containing the workspace is mounted at `/workspaces/<folder>`; default `workspaceFolder`; `resolveWorkspaceMount` |
| `gpu.go` | `GPURequirement` (hostRequirements.gpu) |
| `tree.go` | The JSONC tree a config is read into (BOM, case-sensitive keys), `Substitute` (one pass over every string), decoding |
| `substitute.go` | `SubstitutionContext`, `ResolveContainerEnvRefs` (exec-time `${containerEnv:…}`) |
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
| `state.go` | `ContainerState` enum (Unknown, Absent, Created, Running, Stale, Broken) and `ContainerInfo` |
| `labels.go` | `ContainerLabels` struct, label schema (prefix: `com.griffithind.dcx`) |

#### Features (`internal/features/`)
| File | Purpose |
|------|---------|
| `manager.go` | `Manager.ResolveAll()` - entry point |
| `types.go` | `ParseFeatureSource` (upstream's reference rules, `LegacyFeatureRef`), `Dependencies` (ordered `dependsOn`), `EnvFile` |
| `resolver.go` | Resolve features from local (inside `.devcontainer`), OCI registry (credentials via `registryauth`; manifests verified against pinned digests), or HTTPS; cache in `~/.cache/dcx/features`: immutable `sha256/<hex>` entries (`fetchToStore`, never replaced) plus `refs/` from tags/URLs to digests |
| `ordering.go` | Upstream's round-based install order (`node`, `sameFeature`, `compareFeatures`, override priorities) |
| `dockerfile.go` | Generate Dockerfile for feature installation (feature `containerEnv` becomes `ENV` before its install; `FeatureUsers`) |
| `lock.go` | `GenerateLockfile` / `VerifyLockfile` (upstream's format: keys as written, integrity = manifest digest) |

### Build Layer (`internal/build/`)
| File | Purpose |
|------|---------|
| `builder.go` | `Builder` and `build.Engine`, the narrow engine interface it builds through (it never runs docker itself) |
| `dockerfile.go` | `BuildFromDockerfile` (secrets, build contexts) |
| `features.go` | Build derived images with features (`stageFeatures`) |
| `metadata.go` | The `devcontainer.metadata` label of built images |
| `uid.go` | UID update layer for host/container user matching |

### Supporting Packages
- `internal/lifecycle/` - Hook execution from `devcontainer.Hooks`; `Markers` records completed once-per-container phases
- `internal/lifecyclecmd/` - `Command`, the typed lifecycle command (string / array / named) used by devcontainer.json, image and feature metadata, and its only parser
- `internal/registryauth/` - Registry credentials as docker finds them (`~/.docker/config.json` auths, `credHelpers`, `credsStore`; `GITHUB_TOKEN` for ghcr.io); runs credential helpers, so it is exempt from the os/exec rule
- `internal/mount/` - `Mount` and `Parse`, the only mount string parser (devcontainer.json, image and feature metadata)
- `internal/agent/` - The dcx-agent itself (SSH server, sessions, SFTP, setup); compiled into `cmd/dcx-agent`
- `internal/agentctl/` - Host side of the agent: install/upgrade, SSH secrets, setup and launch (`Provision`)
- `internal/ssh/` - Host keys, known_hosts, authorized-key collection, `hostconfig` (~/.ssh/dcx/config), `client` (in-process SSH for lifecycle hooks only)
- `internal/env/` - `/etc/environment` (the container's `Config.Env`, never `remoteEnv`) and `/etc/profile` patching; `userEnvProbe` (`$SHELL`, else the user's login shell; NUL-separated output)
- `internal/secrets/` - Fetching runtime and build secrets (commands run on the host)
- `internal/lockfile/` - `devcontainer-lock.json` read/write
- `internal/ui/` - Terminal output: `Printf`/`Println`/tables are data on stdout (shown with `--quiet`); `Info`, `Success`, `Warning` and spinners are progress on stderr (silenced by `--quiet`); colour off with `NO_COLOR`, `--no-color` or a non-terminal
- `internal/version/` - Build version
- `internal/shortcuts/` - Shortcut resolution from customizations.dcx
- `internal/agentproto/` - Contract shared by host and agent (binary path, port, secret paths, `SessionConfig`)
- `internal/report/` - `Reporter` interface; library code reports progress through it and never prints
- `internal/shell/` - The only POSIX shell quoter (`Quote`, `Join`)
- `internal/dcxhome/` - dcx's state directory: `$DCX_HOME`, default `~/.dcx`: known_hosts, the fallback client key, and `workspaces/<id>/` per workspace (`WorkspaceDir`: host key, lock, compose override, `workspace.json`); never build `~/.dcx` paths by hand. Docker labels stay the source of truth for container state

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
    ConfigHash, InputsHash string                // Staleness key (hash.config) / drift key (hash.inputs)
}
```

### ExecutionPlan (`devcontainer/plan.go`)
Sealed interface with three implementations:
- `ImagePlan{Image}` - Pre-built image
- `DockerfilePlan{Dockerfile, Context, Args, Target, CacheFrom, Options}` - Custom build
- `ComposePlan{Files, Service, RunServices, ProjectName, Config}` - Docker Compose (`Config` is the compose model resolution read)

### ContainerState (`state/state.go`)
```go
StateUnknown  // "" - not determined
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
- `hash.config` (staleness key), `hash.inputs` (drift key), `hash.version`, `lifecycle.markers`
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
    ↓ LoadConfig: JSONC tree → Raw; every string substituted once (substituteTree) → Local
DevContainerConfig (substituted)               — Local
    ↓ compose model (ComposeConfigSource: docker compose config, every profile)
    ↓ metadataImage: the image, or the FROM of a Dockerfile / compose build (baseimage.go)
    ↓ its devcontainer.metadata (ImageMetadataSource), variable-substituted
    ↓ MergeMetadata                                — Config
    ↓ lockfile (LockfileUse)
    ↓ Builder.Build()
    ├─ NewIdentity (name, resource name / compose project, SSH alias)
    ├─ Create ExecutionPlan
    ├─ features.Manager.ResolveAll() → topological sort
    ├─ Compute hashes
    └─ Merge feature config (mounts, caps, privileged, init; feature containerEnv goes into the image as ENV)
    ↓
ResolvedDevContainer
```
All of it is `devcontainer.ResolveWorkspace`; up, status, build, lock, config and the golden tests resolve through it. Commands that must not resolve (stop, down, ssh) use `LoadIdentity`, which the golden test checks against resolution.

### Container Lifecycle (Up)
```
CLI: dcx up [--recreate] [--rebuild] [--pull] [--hosts]
    ↓ NewCLIContext() → Service.Up(UpOptions)
Service.Up (internal/service/up.go)
    ├─ lockWorkspace ($DCX_HOME/workspaces/<id>/lock; Up, Down, Stop and Restart take it)
    ├─ runInitialize: host initializeCommand on every up, before resolution (it may
    │  generate compose files, a Dockerfile, features)
    ├─ LoadWithOptions → devcontainer.ResolveWorkspace → ResolvedDevContainer
    ├─ dcxhome.WriteWorkspace (workspace.json: path, config, name)
    ├─ StateWithHash + setupComplete + agentctl.Listening
    ├─ decideUp → none | resume | start | create | recreate
    ├─ sshAccessFor (--hosts recorded on the container, kept until recreate)
    └─ steps, each idempotent:
        container    start: startExisting
                     create/recreate (createEnvironment): Provisioner.Prepare
                       (base image → features layer → UID layer (layerImages),
                       compose service builds; returns Prepared, a user image pinned
                       by ID), then DownWithIDs (recreate only, after Prepare
                       succeeded), then Provisioner.Create(Prepared): planSSH (port,
                       listen address, network mode), prepareCommand (feature
                       entrypoints), CreateContainer / compose up with override.
                       Nothing is built after the old environment is removed.
        agent        agentctl.Provision: install, root-owned SSH secrets, setup, launch as root
        secrets      runtime secrets on the /run/secrets tmpfs
        environment  /etc/environment, userEnvProbe, /run/dcx/session.json
        ssh config   ~/.ssh/dcx/config block, known_hosts pin (before the hooks, so a
                     failing hook still leaves a way in)
        hooks        onCreate/updateContent/postCreate once (markers), postStart per start
                     (a PTY when dcx's stdout is a terminal; parallel commands all finish)
                     (also after a restart outside dcx: StartedAt vs the poststart marker)
```

### State Detection
```
StateManager.State()
    ↓ Docker.ListContainers(labels)
Find containers with the workspace.id label
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
`Provisioner.spec` derives a `ContainerSpec` from the resolved config; `dockerRunArgs` (single container) and `composeOverride` (compose) render it. A new runtime setting goes into `ContainerSpec` and both renderers — `TestContainerSpecParity` fails otherwise. The compose override persists at `$DCX_HOME/workspaces/<workspace-id>/docker-compose.dcx.yml` (`~/.dcx` by default; older dcx wrote `compose/<id>/`, which existing environments still reference until recreated); existing compose environments are operated on with the `-f` files compose recorded on the container (`ExistingComposeProject`).

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
- Features install as root (`USER root`); the base image's `USER` (`Engine.ImageConfig`) is restored after them.
- Entrypoints (`resolved.Entrypoints`): those recorded in the base image's metadata, then the features', run before the keep-alive or the original command.
- Mounts: unmodelled `--mount` options (`Mount.Options`) pass through; relative and `~` bind sources resolve against the workspace; the workspace mount is relabelled (`:z`) when Docker labels containers for SELinux (`SystemInfo.SELinux`).
- Labels: a built image carries the FROM image's metadata plus the local config; the features layer adds the features to the metadata resolution read (`resolved.MetadataLabel`).
- Ports (`Provisioner.buildPortBindings`): `forwardPorts` without a host address and `appPort` numbers publish on `127.0.0.1`, `appPort` strings as written; a host port already taken (an earlier entry, or the compose file's own `ports`) is skipped; with compose `network_mode: service:<x>` every port is published on `<x>`; with host networking none are.
- Workspace mount (`resolved.WorkspaceMount`): the git root at `/workspaces/<folder>`, independent of `workspaceFolder`; a non-trivial layout is part of `hash.inputs`.
- The container's `devcontainer.metadata` label is the merged metadata (image, features, local config), as upstream records it. Labels dcx writes keep the base entries verbatim, string mounts as written, and devcontainer.json unsubstituted (`resolved.Raw`).
- Builds run `docker buildx build --load`, or `docker build` (BuildKit) without the buildx plugin; build contexts (features) need buildx.
- Containers are created without `-t -i -w`; the keep-alive is a `sleep 1` loop (`sleepForever`).
- UID layer (`Provisioner.uidUser`): `remoteUser`/`containerUser`, else the runArgs or compose user, else the image's `USER`; skipped for root and numeric users (`devcontainer.UpdatableUser`).
- Each layer is tagged `dcx/<workspace>:<features|uid>-<hash>` where the hash covers the parent image ID plus the layer's inputs (see `Provisioner.layerImages`), and reused when the tag exists. The features layer uses `FeaturesLayerKey` (install order) and the label it writes; `FeaturesKey` (order-free) is only for `hash.inputs`.

### Naming and staleness
- Container / default compose project name: `devcontainer.ResourceName(name, id)` (`<slug>-<id8>`). Never derive Docker names from `name` alone.
- Existing environments are always located by the `workspace.id` label and operated on by container ID / recorded compose project.
- Workspace ID: `WorkspaceID(path, configPath)`, the path-only `ComputeID` for the default config locations and path + config file otherwise, so several configs of one folder are separate environments. `${devcontainerId}` is the same ID.
- `hash.config` is the staleness key from `ComputeConfigHash`: devcontainer.json as written (no substitution), the Dockerfile and compose files, and local feature files. A change recreates the container.
- `hash.inputs` (`ComputeInputsHash`) adds substituted values and feature digests. When only it differs, up and status report drift and keep the container (`--recreate` applies it).
- `hash.version` marks the algorithm; containers with an older version are treated as current.

## Variable Substitution

Supported patterns in `devcontainer.json`:
- `${localEnv:VAR}` / `${localEnv:VAR:default}` - Host environment
- `${containerEnv:VAR}` - Container environment
- `${localWorkspaceFolder}` / `${localWorkspaceFolderBasename}` - Host workspace path / its last element
- `${containerWorkspaceFolder}` / `${containerWorkspaceFolderBasename}` - Container workspace path / its last element
- `${devcontainerId}` - Workspace ID hash
- `${userHome}` - User's home directory
- `${pathSeparator}` - The host's path separator

## Feature Resolution

Features resolved in order:
1. Parse feature map from devcontainer.json (legacy short ids mapped, `maven`/`gradle`/`jupyterlab` folded into options)
2. Resolve each feature (local path, OCI registry, HTTPS tarball); a feature is its source plus options (`sameFeature`), so a dependency with other options installs again
3. Recursively resolve `dependsOn` (fetched when not listed; `installsAfter` only orders features that are listed)
4. Order in rounds as devcontainers/cli does (`orderFeatures`): dependencies first, `overrideFeatureInstallOrder` as a priority within rounds, rounds sorted by `compareFeatures`
5. Generate Dockerfile with staged feature installation; each feature's options are its own `devcontainer-features.env`

OCI features fetched from registries like `ghcr.io/devcontainers/features/go:1`.
Lockfile support for reproducible builds via digest pinning, in devcontainers/cli's format (`lockfile.CompareIDs` orders keys like `localeCompare`); `up`/`build` write it only when it is an empty file.

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

The `dcx-agent` binary is embedded (gzip-compressed) and deployed to `/var/lib/dcx/bin/dcx-agent` (root-owned; never `/tmp`). It runs as root (sessions switch to the session user; without a remoteUser/containerUser that is the container's default user by name; the shell is `$SHELL`, else the login shell, else `/bin/sh`); dcx first runs `dcx-agent setup` to create `/var/lib/dcx` and `/run/dcx`, the shell integration and the log. How it is reached depends on the network mode (published on the container, on the compose `service:` owner, or listening on the host port with `host` networking); `--hosts` is recorded on the container and kept until a recreate changes it. Every SSH session (dcx, OpenSSH, editors) gets `/run/dcx/session.json` (`agentproto.SessionConfig`: userEnvProbe + remoteEnv, postAttachCommand for interactive sessions), written by `up`. Host keys are created only by `up` (`EnsureHostKey`); everything else uses `LoadHostKey`. SSH host blocks live in `~/.ssh/dcx/config`, included from the top of `~/.ssh/config`.

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
| Conformance | `test/conformance/` (spec merge and default rules through `ResolveWorkspace`) | Part of `make test-all` |
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
- Variable substitution happens once, in `devcontainer.LoadConfig`, over every string of the JSON tree before it is decoded (image metadata likewise, in `parseImageMetadata`). Don't substitute again downstream. `${containerEnv:…}` is resolved at exec time with `ResolveContainerEnvRefs`.
- Compatibility with devcontainers/cli is tracked in `docs/compat-review.md`; read it before changing resolution, features or the runtime.
- Container runtime settings live in `container.ContainerSpec`, rendered by both engines; run `docker compose` only through `container.ComposeProject`.
- Parse mounts with `mount.Parse` (`devcontainer.ParseMountString`) and ports with `ParsePortSpec`/`ParsePortSpecs`; lifecycle commands are `*lifecyclecmd.Command`; don't add new parsers or `interface{}` config fields.
- Typed config fields must marshal back to the JSON they were read from: the config hash (`TestConfigHashEncodingIsStable`) is the staleness key.
- Resolution changes are covered by golden files in `internal/devcontainer/testdata/resolve`; review the diff of `go test ./internal/devcontainer -run TestResolveGolden -args -update`.
- Every command's `--help` is pinned in `internal/cli/testdata/help.golden`; after changing commands or flags review the diff of `go test ./internal/cli -run TestHelpGolden -args -update`.
