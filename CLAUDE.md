# CLAUDE.md

Guidance for Claude Code (claude.ai/code) in this repository: dcx, a single-binary Go CLI that runs devcontainers and connects to them over SSH.

## Build, test, verify

```bash
make build          # bin/dcx with the embedded agent binaries (builds the agent first)
make build-agent    # only the dcx-agent Linux binaries (amd64/arm64)
make test           # unit tests
make test-all       # unit + conformance + e2e (e2e needs Docker)
make lint           # golangci-lint: gofmt/goimports, depguard rules, e2e-tagged files
make fmt            # apply the formatters
make deadcode       # fail on unused code

go test -v -run TestShouldUpdateUID ./internal/devcontainer/...
make build && go test -v -tags=e2e -run TestStateLifecycleE2E ./test/e2e/...
```

Before committing: `make build && make lint && make deadcode && make test-all`.

| Tests | Where |
|-------|-------|
| Unit | `internal/*/`; `containertest.Fake` is the in-memory engine |
| Conformance | `test/conformance/`: spec merge/default rules through `ResolveWorkspace`, and `testdata/official/` cases compared with the official CLI's recorded output (re-record by hand with `record-official.sh`) |
| E2E | `test/e2e/`, helpers in `test/helpers/` |
| Goldens | resolution `internal/devcontainer/testdata/resolve` (`TestResolveGolden`), `--help` `internal/cli/testdata/help.golden` (`TestHelpGolden`), status/list rendering `internal/cli/testdata`; regenerate with `go test <pkg> -run <Test> -args -update` and review the diff |

E2E tests run commands with `dcx ssh -- …` (OpenSSH and the generated config); `helpers.RunDCXInPTY` runs interactive sessions (killed after two minutes). `TestMain` calls `helpers.Isolate`: `DCX_HOME` and `DCX_SSH_DIR` point at temporary directories and `SSH_AUTH_SOCK` at an in-process agent with a generated key (`helpers.AgentKeyFingerprint`), so a run never touches the developer's `~/.dcx`, `~/.ssh` or agent. `CreateTempWorkspace`, `CreateTempComposeWorkspace` and `CreateWorkspaceFiles` run `dcx down` when the test ends. Use pinned image tags (`alpine:3.24`).

## Architecture

Layers, top to bottom: **cli** (Cobra commands; only it and `ui` print) → **service** (`DevContainerService`: lifecycle, hooks, SSH) → **container** (`Engine`, `Provisioner`) → **domain** (`devcontainer`, `state`, `features`). Container state lives only in Docker labels.

Entry points: `cmd/dcx` (`cli.Execute`), `cmd/dcx-agent` (`agent.Execute`), `agent-embed.go` (embeds `agentbin/`, where `make build-agent` writes the gzipped agent binaries; they are not committed, so a clean checkout compiles but `dcx up` needs `make build`).

### cli (`internal/cli/`)

| File | Purpose |
|------|---------|
| `root.go` | Root command, global flags (`--workspace`, `--config`, `--quiet`, `--verbose`, `--no-color`) |
| `context.go` | Composition root: `commandContext` (Ctrl-C aware), `newEngine`/`newDaemonEngine` (the only engine construction; `-v` traces docker commands), `newService`, `CLIContext` |
| `validate.go` | `RequireRunningContainer`, `RequireExistingContainer` |
| `up.go`, `down.go`, `stop.go`, `restart.go` | Lifecycle commands |
| `ssh.go` | `dcx ssh [-- cmd]`: execs OpenSSH to the workspace alias |
| `status.go`, `list.go`, `logs.go` | Information; render what the service returns |
| `build.go`, `clean.go`, `lock.go`, `config.go`, `doctor.go`, `completion.go` | The other commands |
| `hosts_parser.go` | `--hosts` parsing |
| `output.go` | `printJSON`; with `--json`, `Execute` prints errors as `{"error": …}` |

Commands get their context, engine and service from `context.go` and never construct a Docker client or service themselves, so command tests run against `containertest.Fake` (`commands_engine_test.go`):

```go
cliCtx, err := NewCLIContext() // engine, service, identity
if err != nil { return err }
defer cliCtx.Close()
changed, err := cliCtx.Service.Up(cliCtx.Ctx, service.UpOptions{...})
```

Commands that must work without a daemon use `commandContext` + `newEngine()` + `newService`.

### service (`internal/service/`)

| File | Purpose |
|------|---------|
| `devcontainer.go` | `DevContainerService`: `Load`/`LoadWithOptions` (resolve through `devcontainer.ResolveWorkspace`), `Down`/`DownWithIDs`, `Build` (Prepare alone, same secrets and flags as `up`), `Lock` |
| `up.go` | `Up` (returns whether there was anything to do): `planUp`, `decideUp`, `announceUp`, the steps |
| `status.go` | `Status` (state, drift, SSH endpoint) and `List` |
| `stop.go` | `Stop` (honours `shutdownAction`), `Restart` (stop, then `up`) |
| `lock_workspace.go` | `lockWorkspace` (`$DCX_HOME/workspaces/<id>/lock`; Up, Down, Stop and Restart take it), `tryLockWorkspace` |
| `clean.go` | `dcx clean`: unused images and `PruneWorkspaces` (workspace folders and older-layout files without a container) |

**Up** (`up.go`) is the only start path (`dcx up` on a stopped container, `dcx restart`), so the agent, runtime secrets and postStart are always re-established. Every step is idempotent; a failed or interrupted Up resumes on the next run.

```
lockWorkspace
runInitialize        host initializeCommand, read from devcontainer.json alone, before resolution
planUp               LoadWithOptions → ResolveWorkspace (--pull: pull the base image first, so its
                     metadata applies); dcxhome.WriteWorkspace (workspace.json); initLockfile;
                     StateWithHash + setupComplete + agentctl.Listening;
                     decideUp → none | resume | start | create | recreate; announceUp;
                     checkHostRequirements; keepComposeProject; sshAccessFor (--hosts, kept until recreate)
container            start: startExisting
                     create/recreate (createEnvironment): Provisioner.Prepare (base image →
                     features layer → UID layer, compose service builds; returns Prepared, the
                     image pinned by ID), DownWithIDs (recreate only, after Prepare succeeded),
                     Provisioner.Create(Prepared): planSSH, prepareCommand, docker create /
                     compose up with the override. Nothing is built after the old environment
                     is removed.
agent                runtime secrets fetched on the host; agentctl.Provision: digest-checked install
                     (probe, copy, install), then one `dcx-agent provision` exec as root with an
                     agentproto.ProvisionRequest on stdin: root-owned SSH secrets, runtime secrets on
                     the /run/secrets tmpfs, /etc/environment, /etc/profile, userEnvProbe,
                     /run/dcx/session.json, and the listener (started detached, waited for)
ssh config           ~/.ssh/dcx/config block and known_hosts pin (before the hooks, so a failing
                     hook still leaves a way in)
hooks                onCreate/updateContent/postCreate once (markers in /var/lib/dcx/lifecycle),
                     postStart per start (also after a restart outside dcx: StartedAt vs the
                     poststart marker); run as `ssh <alias>`, -tt when dcx's stdout is a terminal
```

### container (`internal/container/`)

| File | Purpose |
|------|---------|
| `engine.go` | `Engine`: everything service and runtime need from the container engine |
| `docker.go` | `Docker`, the Docker CLI implementation (injected, no singleton); the only code that runs `docker` |
| `containertest/` | `Fake` engine for unit tests |
| `provisioner.go` | `Provisioner`: `Prepare` (`layerImages`, compose builds), `Create` (dispatches on the plan), `Start`, `Build`; `spec`, `buildLabels`, `buildPortBindings` |
| `spec.go` | `ContainerSpec`: every runtime setting of the primary container |
| `compose.go` | `ComposeProject`, the only compose executor (explicit `-f` files); `Start` is `up -d --no-recreate`; `ExistingComposeProject` |
| `compose_override.go` | `composeOverride`: a `ContainerSpec` as override YAML |
| `secrets.go` | Mounting secrets after start |
| `entrypoint.go` | Entrypoint chaining (image metadata, then features) and the keep-alive |
| `ssh_endpoint.go` | `planSSH` (agent port, listen address, network mode), `SSHHostPort`/`SSHListenAddr`/`RecordedSSHAccess` |
| `runtime.go` | `UpOptions`, `SSHAccess` (`--hosts`), `DownOptions` |

### devcontainer (`internal/devcontainer/`)

| File | Purpose |
|------|---------|
| `resolve.go` | `ResolveWorkspace`, the one resolution pipeline |
| `parser.go`, `tree.go` | JSONC tree (BOM, case-sensitive keys), `LoadConfig` (`Loaded{Raw, Config}`), `Substitute` (one pass over every string) |
| `config.go`, `types.go` | `DevContainerConfig`; `StringOrSlice`, `StringOrArray`, `StringMap`, `NullableStringMap` |
| `substitute.go` | `SubstitutionContext`, `ResolveContainerEnvRefs` (exec-time `${containerEnv:…}`) |
| `metadata.go` | `parseImageMetadata`, `MergeMetadata` |
| `baseimage.go` | `metadataImage`: the image whose metadata applies (Dockerfile `FROM`, or the compose model) |
| `compose_config.go` | `ComposeConfig`, the compose model (`docker compose config --format json`, every profile, via `ComposeConfigSource`), read by metadata discovery, hashing and provisioning |
| `builder.go` | `Builder.Build`, the build step of resolution |
| `resolved.go`, `plan.go` | `ResolvedDevContainer`; `ExecutionPlan` |
| `id.go`, `sanitize.go` | `NewIdentity`/`LoadIdentity` (the only identity computation), `SanitizeProjectName` |
| `hashes.go` | `hash.config` and `hash.inputs` |
| `hooks.go` | `Hooks`: per-phase lists (image metadata, features, devcontainer.json) |
| `workspace.go` | Git-root workspace mount, default `workspaceFolder` |
| `ports.go` | `ParsePortSpec`/`ParsePortSpecs` (ranges), `PortForward.Collides` |
| `runargs.go`, `hostreqs.go`, `gpu.go` | `ParseRunArgs`, `HostRequirementWarnings`, `GPURequirement` |
| `dcx.go`, `validate.go` | `customizations.dcx` (secrets, buildSecrets), `Validate` |

### Other packages

- `state/`: `StateManager` (`State`, `StateWithHash`) finds containers by the `workspace.id` label; `ContainerState` (Unknown, Absent, Created, Running, Stale, Broken); `ContainerLabels`.
- `features/`: `Manager.ResolveAll`; `ParseFeatureSource` (upstream's reference rules, legacy ids); `resolver.go` (local, OCI with `registryauth` credentials and digest-verified manifests, HTTPS; cache in `~/.cache/dcx/features`: immutable `sha256/<hex>` entries plus `refs/` from tags and URLs to digests, re-checked after 24 h); `ordering.go` (upstream's rounds: `orderFeatures`, `sameFeature`, `compareFeatures`); `dockerfile.go` (features layer, per-feature env files); `lock.go` (`GenerateLockfile`, `VerifyLockfile`).
- `build/`: `Builder` over the narrow `build.Engine`; `dockerfile.go`, `features.go`, `metadata.go` (the image's `devcontainer.metadata` label), `uid.go` (UID layer).
- `lifecycle/`: `HookRunner` (host hooks with `os/exec`, container hooks with `ssh <alias>`), `Markers`. `lifecyclecmd/`: `Command` (string / array / named), the only lifecycle-command parser.
- `agent/`: the dcx-agent (SSH server, sessions, SFTP, `provision`, IP gate). `agentctl/`: its host side (`Deploy`, `Provision`). `agentproto/`: the shared contract (paths, port, secret locations, `SessionConfig`).
- `ssh/`: host keys, known_hosts, authorized keys, port choice; `hostconfig` writes `~/.ssh/dcx/config`.
- `dcxhome/`: `$DCX_HOME` (default `~/.dcx`): known_hosts, the fallback client key, `workspaces/<id>/` (`WorkspaceDir`: host key, lock, compose override, `workspace.json`). Never build these paths by hand.
- `env/`: stdlib-only logic dcx and the agent share: dcx's `/etc/environment` block (the container's `Config.Env`, never `remoteEnv`), the `/etc/profile` PATH patch, the `userEnvProbe` command and output. `secrets/`: running secret commands on the host. `lockfile/`: `devcontainer-lock.json` I/O. `mount/`: `Mount`, `Parse`. `registryauth/`: docker credentials (runs credential helpers). `shell/`: `Quote`, `Join`. `report/`: `Reporter`. `ui/`: terminal output (data on stdout, progress on stderr, ANSI colour only on a terminal). `version/`.

## Key types

- **`ResolvedDevContainer`**: `Identity` (WorkspaceID, Name, ResourceName, SSHHost); `ConfigPath`, `ConfigDir`, `LocalRoot`; `Config` (merged with image metadata), `Local` (as written, substituted), `Raw` (unsubstituted); `Plan`; `BaseImage`, `WorkspaceFolder`, `WorkspaceMount`; `RemoteUser`, `ContainerUser`, `EffectiveUser`, `ShouldUpdateUID`; `ContainerEnv`, `RemoteEnv`; `Mounts`, `CapAdd`, `Privileged`, `Init`, `RunArgs`, `ForwardPorts`, `AppPorts`; `Hooks`, `Entrypoints`; `Features`, `MetadataLabel`; `RuntimeSecrets`, `BuildSecrets`; `ConfigHash`, `InputsHash`. Read the resolved fields (`OverrideCommand`, `HostRequirements`, `UserEnvProbe`), not `Config`.
- **`ExecutionPlan`** (sealed with `sealed()`): `ImagePlan{Image}`, `DockerfilePlan{Dockerfile, Context, Args, Target, CacheFrom, Options}`, `ComposePlan{Files, Service, RunServices, ProjectName, Config}`. Type switches over it have a `default:` error branch.
- **Labels** (prefix `com.griffithind.dcx`): `schema.version`, `managed`; `workspace.id`, `workspace.name`, `workspace.path`, `config.path`; `hash.config`, `hash.inputs`, `hash.version`, `lifecycle.markers`; `build.method`, `build.base.image`, `build.derived.image`, `features.installed`; `compose.project`, `compose.service`, `container.primary`; `shutdown.action`; `ssh.port`, `ssh.listen`, `ssh.allow` (the endpoint chosen at create time: read it with `SSHHostPort`/`SSHListenAddr`/`RecordedSSHAccess`, never ask Docker for the published port). Labels are immutable after create: add one only if `buildLabels` writes it.

## Resolution

```
devcontainer.json (JSONC) → tree → Raw; every string substituted once → Local
→ compose model (compose configs) → metadataImage → its devcontainer.metadata (substituted)
→ MergeMetadata → Config → lockfile → Builder.Build: NewIdentity, plan, features
  (ResolveAll, round order), hashes, feature config (mounts, caps, privileged, init;
  feature containerEnv becomes image ENV) → ResolvedDevContainer
```

Up, status, build, lock, config and the goldens all resolve through `ResolveWorkspace`. Commands that must not resolve (stop, down, ssh) use `LoadIdentity`, which a golden test checks against resolution.

Substitution: `${localEnv:VAR[:default]}`, `${containerEnv:VAR}` (at exec time), `${localWorkspaceFolder[Basename]}`, `${containerWorkspaceFolder[Basename]}`, `${devcontainerId}` (the workspace ID), `${userHome}`, `${pathSeparator}`.

## Images and runtime

- Pipeline: base (pulled, or built from the Dockerfile on every create as `dcx/<id>:base`; BuildKit's cache makes it cheap) → features layer → UID layer. Each layer is tagged `dcx/<id>:<features|uid>-<hash>`, the hash covering the parent image ID and the layer's inputs (`Provisioner.layerImages`), and reused when the tag exists. The features layer keys on `FeaturesLayerKey` (install order) and its label; `FeaturesKey` (order-free) is only for `hash.inputs`.
- Features install as root; the base image's `USER` (`Engine.ImageConfig`) is restored after them. Builds run `docker buildx build --load`, or `docker build` without buildx (the features layer needs buildx).
- UID layer (`Provisioner.uidUser`): `remoteUser`/`containerUser`, else the runArgs or compose user, else the image's `USER`; skipped for root and numeric users (`devcontainer.UpdatableUser`).
- Entrypoints (`resolved.Entrypoints`): those in the base image's metadata, then the features', before the keep-alive (a `sleep 1` loop, `sleepForever`) or the original command. Containers are created without `-t -i -w`.
- Mounts: unmodelled `--mount` options pass through (`Mount.Options`); relative and `~` bind sources resolve against the workspace; the workspace mount (`resolved.WorkspaceMount`: the git root at `/workspaces/<folder>`, independent of `workspaceFolder`) is relabelled `:z` under SELinux (`SystemInfo.SELinux`).
- Ports (`buildPortBindings`): `forwardPorts` without a host and `appPort` numbers on `127.0.0.1`, `appPort` strings as written; a host port already taken (an earlier entry, or the compose file's `ports`) is skipped; compose `network_mode: service:<x>` publishes on `<x>`; host networking publishes nothing.
- Labels on images and containers: `devcontainer.metadata` is the merged metadata as upstream records it, with the base entries verbatim, string mounts as written and devcontainer.json unsubstituted (`resolved.Raw`).
- `ContainerSpec` is the single source of runtime settings: `Provisioner.spec` derives it, `dockerRunArgs` and `composeOverride` render it, and `TestContainerSpecParity` fails when a setting is missing from either. The compose override lives at `$DCX_HOME/workspaces/<id>/docker-compose.dcx.yml` (older dcx wrote `compose/<id>/`, which existing environments use until recreated). Existing compose environments are operated on with the `-f` files compose recorded (`ExistingComposeProject`).

## Naming and staleness

- Container / default compose project: `devcontainer.ResourceName(name, id)` (`<slug>-<id8>`). Never derive Docker names from `name` alone. Existing environments are found by the `workspace.id` label and operated on by container ID or recorded compose project.
- Workspace ID: `WorkspaceID(path, configPath)`: the path-only `ComputeID` for the default config locations, path + config file otherwise, so several configs of one folder are separate environments.
- `hash.config` (`ComputeConfigHash`) is the staleness key: devcontainer.json as written, the Dockerfile and compose files, local feature files. A change recreates the container.
- `hash.inputs` (`ComputeInputsHash`) adds substituted values and feature digests. When only it differs, up and status report drift and keep the container.
- `hash.version` marks the algorithm; containers with an older version are treated as current.

## Agent

`dcx-agent` is embedded gzipped and deployed to `/var/lib/dcx/bin/dcx-agent` (root-owned, never `/tmp`), checked by a SHA-256 sidecar. It runs as root; sessions switch to the session user (`remoteUser`/`containerUser`, else the container's default user by name) with `$SHELL`, else the login shell, else `/bin/sh`. Everything it needs arrives in one `dcx-agent provision` call (`agentproto.ProvisionRequest` on stdin, `ProvisionResult` on stdout), which creates `/var/lib/dcx`, `/run/dcx`, the shell integration and the log, writes the secrets (atomically, never through a symlink), `/etc/environment`, `/etc/profile` and the session configuration, and starts the listener. It is reached on the container's published port, the compose `service:` owner's, or the host port with host networking. Every session gets `/run/dcx/session.json` (`agentproto.SessionConfig`: userEnvProbe + remoteEnv, postAttachCommand for interactive sessions), written by `up`. Host keys are created only by `up` (`EnsureHostKey`); everything else uses `LoadHostKey`. Subcommands: `provision`, `listen`, `ping`, `sftp-server` (stdlib `flag`, no Cobra, to keep it small).

## Rules

- Everything reaches the container over OpenSSH through the workspace's alias: users (`dcx ssh`, `ssh <alias>`) and container hooks (`lifecycle.HookRunner`). dcx has no SSH client of its own.
- Only `internal/cli` and `internal/ui` print. Everything else takes a `report.Reporter`; `--json` commands pass `report.Discard`.
- Library code receives a `container.Engine` (`build` the narrower `build.Engine`) and never constructs one. Only `container/docker.go` runs `docker` (depguard enforces it): add an Engine method rather than calling `os/exec`.
- Container-side setup belongs in the agent (`provision`), in Go; keep `sh -c` scripts to what must run before the agent exists (deploy) or around it (markers). Build shell command lines with `shell.Quote`/`shell.Join`; never interpolate values into `sh -c` scripts (pass them as positional args).
- Agent paths, ports and secret locations come from `agentproto`.
- Substitution happens once, in `devcontainer.LoadConfig` (image metadata in `parseImageMetadata`); never substitute again downstream.
- Read `docs/compatibility.md` before changing resolution, features or the runtime; record a new difference from devcontainers/cli there.
- Runtime settings live in `ContainerSpec`; run `docker compose` only through `ComposeProject`.
- Parse mounts with `mount.Parse`, ports with `ParsePortSpec`/`ParsePortSpecs`, lifecycle commands as `*lifecyclecmd.Command`; no new parsers or `interface{}` config fields.
- Typed config fields must marshal back to the JSON they were read from: the config hash is the staleness key (`TestConfigHashEncodingIsStable`).
- Changing resolution or commands means reviewing the golden diffs (see Tests).
