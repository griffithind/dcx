# dcx audit and refactor plan

> **Historical.** All of this is done and later superseded by the [second](second-audit.md) and [third](third-audit.md) audits. The code may have changed since. For the current architecture and rules, see [CLAUDE.md](../CLAUDE.md).

_Audit date: 2026-11-13, commit `d8e5673`._

## How this was produced

- **Tooling.** Ran `go vet`, `staticcheck`, `deadcode`, `go test -race` and coverage. All are clean apart from coverage. `golangci-lint` isn't installed locally.
- **Manual review.** Four parallel reviewers read every non-test file in four areas:
  - service + container
  - config / features / build / state
  - CLI and support packages
  - SSH / agent / secrets
- **Verification.** Items marked ✅ were re-checked in the source by hand. The rest come from careful line-by-line tracing but were not individually re-checked.
- **E2E.** The e2e suite was run against local Docker. See *E2E results* at the end.

**Summary:** the linters are clean, so the problems aren't style. They are **logic bugs from parallel code paths that drifted apart**. The pattern repeats everywhere: `Up` vs `QuickStart` vs `restart`, compose vs single container, `build` vs `up`, `debug` vs `up` resolution, 4 substitution sites, 3 shell quoters, 3 port parsers, 2 exec paths, 2 compose executors. Coverage is lowest where this happens (container 4%, service 9.5%, cli 9%, ssh/exec 0%). Tests don't catch the drift.

---

## 1. Findings, ranked

### P0: security

| # | Issue | Where |
|---|---|---|
| S1 ✅ | **Privilege escalation inside the container.** `authorize` also trusts `~<client-user>/.ssh/authorized_keys`, but every session runs as the configured user (or root). Any process can add its own key and get a shell as remoteUser (often a sudoer) or root. | `ssh/server/server.go:187` |
| S2 ✅ | **SFTP runs as root.** It is served in-process and never drops to the session user. Any authorized key gets root filesystem access, and IDE-created files end up owned by root. | `ssh/server/server.go:458` |
| S3 ✅ | **Shell injection and path traversal when writing secrets.** `sh -c "cat > %q"` uses Go quoting inside a shell, so `$(...)` still expands, and `filepath.Join(SecretsDir, name)` accepts `../`. A config-controlled secret name gives root code execution in the container. | `container/docker.go:982`, `container/secrets.go:347` |
| S4 ✅ | **Host private key on the overlay filesystem.** `/run/secrets` is tmpfs only when runtime secrets exist. Otherwise the SSH host key lands in the writable layer, where `docker commit`/`export` can see it. | `container/unified.go:545` |
| S5 ✅ | **Feature tar extraction follows symlinks** out of the cache dir, and the blob digest isn't checked. | `features/resolver.go:649` |
| S6 | Agent forwarding socket is owned by root (0700), so it doesn't work for the dropped-privilege user. Temp dirs leak. | `ssh/server/server.go:225` |
| S7 | `/tmp/.dcx-shell` is written by root into a world-writable directory with no ownership or symlink check. | `ssh/server/shell.go:70-121` |
| S8 | Secrets are briefly 0644 before `chmod 400`. | `container/docker.go:982` |
| S9 | Every `~/.ssh/*.pub` is authorized, and the host agent is forwarded into repo-controlled lifecycle hooks. | `ssh/exec` |

### P0: data loss or corruption

| # | Issue | Where |
|---|---|---|
| D1 ✅ | **Any config edit wipes compose named volumes.** Stale, broken, `--recreate` and `--rebuild` all run `down -v`, so your Postgres volume is deleted. | `service/devcontainer.go:369` |
| D2 ✅ | **`~/.ssh/config` block removal is prefix-matched.** Removing `api` swallows everything after `api-v2`'s start marker. The write isn't atomic, and a read error is ignored, after which the file is rewritten with only the dcx block. | `ssh/hostconfig/config.go:73,94,183` |

### P1: core workflows broken

| # | Issue | Where |
|---|---|---|
| W1 ✅ | **`dcx stop && dcx up` / `dcx restart` leave a dead environment.** `QuickStart` and `restart` only run `docker start`. There is no agent relaunch, no postStart hooks, no secret re-mount (tmpfs was wiped), and no ssh config refresh. `exec`, `shell` and `run` then fail. | `service/devcontainer.go:538`, `cli/restart.go:305` |
| W2 | **A half-finished create is never retried.** If a hook or agent deploy fails, the container is left Running with no completion marker, and the next `up` says "already running". | `service/devcontainer.go:377-424` |
| W3 | Race between agent launch and the first SSH dial. There is no readiness wait, and agent startup errors are discarded (`exec -d`). | `service/devcontainer.go:503-534` |
| W4 ✅ | **`dcx up` hangs forever** when a feature has a versioned `dependsOn` key (e.g. `…/pipx-package:1`). The loop is unbounded and the "resolved" check never matches. | `features/manager.go:104-160` |
| W5 ✅ | **Cached derived image skips the UID layer**, so the second `up` runs with the wrong UID. | `container/unified.go:155-159` |
| W6 | **The UID layer tag `<base>-uid<N>` ignores the user, GID and digest.** It is reused across workspaces, kept stale after `--pull`, and invalid for `@sha256` images. | `container/unified.go:334` |
| W7 ✅ | **Same `name` → container name collision.** The container name comes from `name`, not the workspace ID, so two clones or worktrees can't coexist. | `devcontainer/builder.go:200` |
| W8 | **Compose path silently drops** capAdd, securityOpt, privileged, init, feature env/security, GPU, appPorts and overrideCommand. docker-in-docker under compose fails. | `container/unified.go:766-855` |
| W9 ✅ | **Feature `ENV` gets a literal `${PATH}`** (go/node features), because `ShellQuote` escapes `$`. The same value is re-applied at runtime as `-e`, which also overrides the user's `containerEnv`. | `features/dockerfile.go:94`, `container/unified.go:430` |
| W10 | `${devcontainerId}` is unresolved in Parse, and feature mounts are never substituted, so the docker-in-docker volume name is literal. | `devcontainer/parser.go:132`, `builder.go:270` |
| W11 | `--config` skips most substitution, so `dcx -c X up` resolves differently from `dcx up`. | `devcontainer/parser.go:108` |
| W12 | `${containerEnv:VAR}` is never resolved. `remoteEnv.PATH` becomes literal and breaks every command. | `devcontainer/substitute.go` |
| W13 | Most `runArgs` (`--memory`, `--cpus`, `-e`, `--gpus`, `--cap-add`, `-v`…) are silently dropped, and `--shm-size=2gb` parses to 0. | `devcontainer/runargs.go` |
| W14 | Image metadata merge is first-wins (the spec says last-wins). It only runs when the image is already local, so the first and second `up` resolve differently. | `devcontainer/metadata.go:53` |
| W15 ✅ | `initializeCommand` runs after create and only on create. The spec says before build, on every up. | `lifecycle/hooks.go:190` |
| W16 ✅ | `postAttachCommand` is never executed. | `lifecycle` |
| W17 | Shortcut strings are split with `strings.Fields` and each token is quoted, so `cd web && npm test` breaks. | `shortcuts/resolver.go:46` |
| W18 | Global flags are parsed out of shortcut/`run` args. `-q` and `--no-color` are ignored after a local flag. `doctor` shadows `--config`. | `cli/root.go:52`, `cli/doctor.go` |

### P2: staleness and caching

- **H1.** The hash is computed over the raw JSON bytes. Comment, whitespace and `customizations` edits cause a recreate, which, combined with D1, means data loss. `${localEnv:...}` in image/args is not reflected in the hash.
- **H2.** The hash misses local feature contents, Dockerfile context files and the base image digest. The derived-image existence check runs before the base build, so the base is never rebuilt without `--rebuild`.
- **H3.** The generated Dockerfile's ARG/ENV order comes from map iteration, which is non-deterministic and causes BuildKit cache misses.
- **H4.** `--recreate` always forces a rebuild. `--pull` is a no-op on existing containers.
- **H5.** `dcx build` builds something different from `up`: no features or UID layer for single containers.
- **H6.** ✅ The env probe cache reads container labels but writes to `/var/lib/dcx`, so it never hits.
- **H7.** The feature cache isn't written atomically, and a partial directory is accepted forever. The agent deploy (`test -f`) has the same problem, plus no version check, so an upgraded dcx keeps running the old agent.

### P3: smaller bugs

- **Images.** ✅ `dcx clean` filters `dcx-derived/` but images are `dcx/…`, so it never removes anything. `dcx-derived-temp:*` tags accumulate.
- **Labels.** Labels are parsed by splitting on `,`, which breaks the JSON labels and paths containing commas.
- **Status/debug.** ✅ `status --detailed` filters on `dcx.` instead of `com.griffithind.dcx.`. `debug` resolves differently from `up`, so it can report false staleness.
- **Ports.** `forwardPorts: "db:5432"` is misparsed. Deterministic ports can collide with stopped containers.
- **Features.** Digest-pinned feature refs are misparsed. `overrideFeatureInstallOrder` matches short IDs only. Hard-dependency ordering is ignored for full refs.
- **Env.** Probed env overrides remoteEnv. The client-sent `USER` overrides the server's correct value.
- **Exec.** A PTY is allocated when stdout is piped. Ctrl-C isn't propagated in non-TTY mode, and signals and disconnects aren't handled server-side. Exit status -1 is sent as 0xFFFFFFFF.
- **Output.** `fmt.Printf` from the feature resolver corrupts `--json` output.
- **Upgrade.** `upgrade` has no checksum check and can downgrade.
- **Hooks.** Cancelling hooks doesn't stop in-flight commands. `waitFor` is cosmetic.
- **Dead code.** SSH key-drift detection reads a label that is never written. The project-name state lookup can never match.

### Build and test infrastructure

- ✅ `make test` doesn't depend on `build-agent`, so on a clean checkout 3 packages fail to compile (`go:embed` pattern has no files).
- ✅ CI runs only on `v*` tags. There is no PR or push CI, so regressions ship until release day. `golangci-lint@latest` is unpinned.
- ✅ The `test-integration` target points at a directory that doesn't exist. `docs` writes to a missing `docs/api`.
- ✅ "Conformance" tests are parse-only unit tests, not spec behaviour tests.
- CLAUDE.md is out of date (e.g. it mentions `cli/exec_builder.go`, which no longer exists).

---

## 2. Root causes

Most of the bugs above trace back to six structural problems. Fixing the bugs one by one without addressing these will just produce the next batch.

1. **No single owner per operation.** "Bring a container to running and usable" is implemented three times (`Up`/StateCreated, `QuickStart`, `restart`), and only one copy is complete. The same goes for compose vs single, build vs up, debug vs up. → **One path per concern.**
2. **Lifecycle is a switch statement, not a reconciler.** `Up` branches on observed state and runs some steps per branch. Nothing records which steps completed, so a failure midway leaves an unrecoverable state (W1, W2, W3, H4). → **Idempotent `Ensure*` steps plus completion markers.**
3. **Identity and hashing are one opaque value.**
   - Name, ID and project are derived inconsistently (W7, the dead project lookup).
   - One raw-bytes hash is used both for "recreate the container" and "rebuild the image" (H1, H2, D1 amplification).
   → **An explicit `Identity` type, and separate semantic `BuildKey` / `RuntimeKey` hashes.**
4. **Resolution is impure and scattered.**
   - `Builder.Build` mixes parsing, IO (`os.Getuid`, network, image inspect) and merging.
   - Substitution happens in 4 places, merges run twice, and there are 3 port parsers and 2 mount parsers.
   → **A linear pure pipeline: discover → parse (typed) → substitute (once) → merge → resolve.**
5. **No seams.**
   - The `MustDocker()` singleton and direct `exec.Command("docker", …)` everywhere make container/service/ssh untestable, hence the 0–10% coverage.
   - Library packages print to stdout.
   → **A `ContainerEngine` interface with a fake, and a `Reporter` interface.**
6. **The agent has no privilege model.**
   - It runs as root.
   - Authorization accepts any home directory.
   - SFTP is in-process.
   - Secret placement is inconsistent.
   → **Explicit privilege dropping for every channel, dcx-managed keys only, secrets always on tmpfs.**

---

## 3. Target architecture

```
cmd/dcx            cmd/dcx-agent
   │                    │
internal/cli  ─────────►│   thin Cobra: parse flags → call app → render (only place that prints)
   │                    │
internal/app            │   use cases: Up Start Stop Restart Down Exec Build Clean Status Diagnose
   ├─ reconcile         │   ordered idempotent steps + per-container completion markers
   │                    │
   ├── config           │   discover · parse (typed model) · substitute (one pass, all fields) · validate
   ├── resolve          │   merge image-metadata/features (last-wins) → Resolved; Identity; BuildKey/RuntimeKey  (pure)
   ├── features         │   fetch (oci/http/local, atomic cache, lockfile) · order · dockerfile gen
   ├── image            │   base → features → uid; tags derived from BuildKey; build == up
   ├── spec             │   Resolved → ContainerSpec  (single source of truth for runtime settings)
   ├── engine           │   ContainerEngine interface; docker impl renders ContainerSpec → `docker create` args,
   │                    │   compose impl renders ContainerSpec → override YAML (yaml.v3); fake impl for tests
   ├── workspace        │   label schema (JSON via inspect, not comma-split) · observed state detection
   ├── lifecycle        │   hook model & runner (phases incl. initialize-on-host and postAttach)
   ├── env              │   env model, precedence rules, probe (+ working cache)
   ├── secrets          │   fetch + mount (tmpfs always, stdin write, validated names, 0400 from creation)
   ├── agent            │   host side: Deploy (atomic, versioned) · Ensure (launch + readiness) · Dial · Exec client
   ├── hostssh          │   ~/.ssh/dcx/config via Include (atomic writes) · host keys · known_hosts
   └── report           │   Reporter interface (progress/log/warn); ui impl in cli, silent impl for --json
                        │
internal/agentproto ◄───┘   constants shared by host & agent: port, paths, label keys, protocol version
internal/agentd             in-container server: SSH, SFTP (as target user), agent fwd, PTY, signals
internal/shell              the one shell-quoting implementation (+ tests)
```

**Dependency rules** (enforce with `depguard` in golangci-lint):

- `config` and `resolve` import no IO packages. Image metadata and feature metadata come in through small interfaces.
- Only `engine` executes `docker`.
- Only `cli` imports `ui`. Everything else takes a `report.Reporter`.
- `agentd` imports only `agentproto` (keeps the binary small and the boundary explicit).

**Removed:** `internal/common`, `internal/util` (dissolved into their owners), `internal/errors` (unused; or adopt it properly with sentinel errors and exit codes), the dead typed models in `devcontainer/types.go`, `CacheData`, `lastResolved`, `ContainerRuntime` (single implementation), and the `UnifiedRuntime` dual-mode struct.

### The key abstractions

**Identity.** One function, one source of truth:

```go
type Identity struct {
    WorkspaceID string // hash(abs workspace path + config path) – stable, unique per checkout
    DisplayName string // from "name", for humans only
    ContainerName string // "dcx-" + WorkspaceID[:12]  (never derived from name)
    ComposeProject string
}
```

**Keys.** Hashes of the *resolved, normalized* model, not the bytes on disk:

```go
type Keys struct {
    Build   string // base image digest | dockerfile+context files | feature contents+options | uid inputs
    Runtime string // ContainerSpec (mounts, env, caps, ports, user, entrypoint…) + Build
}
```

- Editing a comment changes nothing.
- Changing a mount → recreate the container, reuse the image.
- Changing a feature → rebuild.
- **Recreate never removes named volumes.** `down -v` only on explicit `dcx down --volumes`.

**ContainerSpec.** All runtime settings in one struct, rendered by both engines. Compose parity follows by construction, and W8 can't come back.

**Reconciler:**

```go
steps := []Step{
    RunInitializeOnHost,      // every up, before build
    EnsureImage,              // by Keys.Build
    EnsureContainer,          // create/recreate by Keys.Runtime (keeps volumes)
    EnsureRunning,
    EnsureSecrets,            // tmpfs, every start
    EnsureAgent,              // deploy if version differs, launch, wait ready, surface log on failure
    RunHooks(OnCreate, UpdateContent, PostCreate), // once per container – marker label/file
    RunHooks(PostStart),      // every start
    EnsureHostSSHConfig,
}
```

`up`, `start` and `restart` all run the same list. Each step checks before acting, so an interrupted `up` just resumes. `QuickStart` disappears. `exec`/`shell`/`run` call `EnsureAgent` cheaply (a ping) before dialing.

---

## 4. Migration plan

Ship incrementally. Every phase leaves `main` releasable. **Don't do a big-bang rewrite**: the e2e suite is your safety net, so grow it first.

### Phase 0: safety net (1–2 days)

- [x] `make test` (and `vet`, `deadcode`, `test-coverage`) depend on `build-agent`.
- [x] Add PR/push CI (`.github/workflows/ci.yml`): lint, deadcode, unit, conformance, e2e. golangci-lint pinned to v2.14.0 via `make lint`, used by both workflows.
- [x] Failing tests written first for every Phase 1 fix (`test/e2e/safety_e2e_test.go` plus unit tests). Still to add with later phases: two clones with the same name, features + UID across down/up, feature PATH, `postAttach`, `initialize` timing.
- [x] Removed the stale `test-integration` and `docs` targets; updated CLAUDE.md.

### Phase 1: stop the bleeding, in place (2–4 days)

Small targeted patches on today's code, each with a test. Do these before any restructuring because they are security or data-loss issues.

- [x] S1: authorize only the dcx-managed `authorized_keys`.
- [x] S2: SFTP re-execs `dcx-agent sftp-server` under the target user's credentials.
- [x] S3/S8: the path is passed as `$1` (never interpolated), files are created with `umask 077`, and names are validated before the secret command runs.
- [x] S4: `/run/secrets` is always tmpfs. Existing containers pick this up on their next recreate.
- [x] S5: no writes through symlinks, symlink targets confined to the cache dir, setuid bits dropped, OCI layer digest verified.
- [x] D1: recreate never removes named volumes. `docker rm -v` still drops anonymous volumes of single containers.
- [x] D2: exact-line markers, unterminated blocks left intact, atomic symlink-preserving write, read errors propagated.
- [x] W4: refs normalized, each ref resolved once, pass limit, user options never overwritten by a dependency.
- [x] W1: `QuickStart` is deleted. `up` and `restart` go through `Service.Up`. Runtime secrets are re-mounted on every start, and Up waits for the agent to be ready (W3).
- [x] Extra (needed so the fixes reach existing containers): agent deploy is content-hash versioned and installed atomically, and an outdated running agent is replaced (H7, agent half).

### Phase 2: introduce seams (3–5 days)

- [x] `container.Engine` interface implemented by `*Docker`. `MustDocker()` and the singleton are deleted; the engine is injected into the service, runtime, secrets, hooks and SSH exec (`ssh/exec` no longer imports `container`). `containertest.Fake` added.
- [x] `report.Reporter`, with a terminal implementation in `ui`, `Discard` for `--json` output (`config`, `debug --json`, `ssh info --json`), and a `Recorder` for tests. No package outside `cli`/`ui` prints. Usage is only printed for flag errors.
- [x] `agentproto` holds the agent binary path, port and secret paths; the hard-coded copies are gone.
- [x] `internal/shell` provides `Quote`/`Join`, round-trip tested through `/bin/sh` and fuzzed. `quoteForShell` and the buggy `ShellEscapeSingleQuote` are deleted. The Dockerfile ENV/LABEL escapers moved into the Dockerfile generator unchanged; their ENV semantics are fixed in Phase 4.
- [x] Container labels are read from `docker inspect` JSON.
- [ ] Still shelling out to `docker` outside the engine: `build/` (image builds), `env/probe.go`, `ssh/deploy`, `container/compose.go`, `cli/doctor.go`. Move them behind `Engine` when those areas are reworked (Phases 4–6).

With these seams in place, `service` and `container` become unit-testable. Aim for 60%+ before Phase 5.

### Phase 3: config → resolve pipeline (1 week)

- [x] One substitution pass (`SubstituteConfig`) over every string property, including name, build options, compose files, runArgs, capAdd/securityOpt, ports and lifecycle commands. It runs once in `Load` with a single context (`NewSubstitutionContext`: `devcontainerId`, `userHome`, workspace folders), identically with or without `--config`. The builder's partial second pass is gone. Feature mounts are substituted too. `${containerEnv:VAR}` is resolved at exec time and when patching `/etc/environment`, against the container's inspected environment.
- [x] String mounts are re-parsed after substitution, and `ParseMountString` is the only mount parser (user, metadata and feature mounts). The form is chosen by `=`, so a value containing `:` cannot trigger the short form. Mounts are merged per target.
- [x] One port parser, `ParsePortSpec`, supporting `p`, `h:c`, `ip:h:c`, `ip::c` and `/udp`. `PortForward.DockerSpec()` is used for both `docker run` and compose. `forwardPorts` entries like `db:5432` are skipped with a message instead of getting a random host port.
- [x] Image metadata merge follows the spec (last layer wins, `init`/`privileged` true if any layer sets them, mounts last wins per target, per-key maps). It never mutates the local config. `up` and `build` pull the base image when it is missing, so its metadata applies on the first run too.
- [x] `runArgs`: unknown flags pass through verbatim to `docker run`; `--rm`, `--name` and `-d` are refused with a warning; `--shm-size=2gb` parses; compose configs get a warning that runArgs don't apply.
- [x] Feature `containerEnv` is no longer passed as runtime `-e` (it is image `ENV`), and its `ENV` lines no longer escape `$`, so `${PATH}` expands (W9). `.devcontainer/docker-compose.yml` is no longer treated as a devcontainer.json (B7).
- [x] Golden tests: `internal/devcontainer/testdata/resolve/<case>/` run the real `Load`+`Build`; refresh them with `go test ./internal/devcontainer -run TestResolveGolden -args -update`. E2E tests in `test/e2e/config_resolution_e2e_test.go` cover runArgs, `${devcontainerId}`, feature PATH, `--config` and `${containerEnv:…}`.
- [x] `dockerComposeFile` is typed (`StringOrSlice`). The dead `PortSpec`/`PortSpecs`/`GPUConfig`/`FeatureConfig` models are deleted.
- [ ] Moved to Phase 5: typed lifecycle commands (`LifecycleCommand` is kept for this). They flow through features, build metadata and the hook runner as `interface{}` and are best retyped together with the reconciler.
- [ ] Still open: the image-metadata merge is skipped for Dockerfile and compose plans (it would need the built image first). Image-metadata lifecycle commands are not run.

### Phase 4: identity and keys (2–3 days)

- [x] Container names and default compose project names are `ResourceName(name, id)` = `<sanitized name>-<8-char workspace id>`, so clones and worktrees with the same `name` coexist (W7). An explicit compose `name:` is still respected. The SSH alias stays `<name>.dcx`; collisions there remain possible and are a known limitation.
- [x] Existing environments are found by the workspace-id label (the never-matching project-name lookup is removed). `up` on a stopped environment starts the container ID or compose project recorded in labels. Existing compose environments keep their original project name on recreate, so project-scoped named volumes are never orphaned.
- [x] Staleness key (`ComputeConfigHash`) is computed over the normalized, substituted local config, before image-metadata merging. It ignores comments, formatting, name, customizations, ports attributes, remoteEnv, waitFor/userEnvProbe/shutdownAction, and start/attach hooks. It includes local feature file contents (`FeaturesKey`), so editing `install.sh` triggers a rebuild (H1, H2).
- [x] `hash.version` label: containers created by older dcx are treated as current (no mass recreate on upgrade), with a hint to run `dcx up --recreate`.
- [x] Image pipeline: the base build always runs (BuildKit's cache makes it cheap), so Dockerfile context changes and `--pull` take effect. Features and UID layers are tagged `dcx/<workspace>:<kind>-<hash(parent image ID + layer inputs)>`, so the UID layer is applied on cache hits (W5) and keyed by user/UID/GID (W6). Valid for digest-pinned images.
- [x] `dcx clean` matches `dcx/*` (plus legacy `dcx-derived*`) and removes by reference; images in use by containers are kept.
- [x] Feature Dockerfile is deterministic (sorted ARG/ENV), each feature's ENV follows its install so later features see it, and the `# syntax=` frontend pin (a Docker Hub pull on every build) is gone.
- [ ] Not done: the base image digest is not part of the staleness key (a newer upstream image doesn't recreate the container by design); `--rebuild` doesn't pass `--no-cache` to the base Dockerfile build.

### Phase 5: reconciler (1 week)

- [x] `Up` (`internal/service/up.go`) is an ordered list of idempotent steps: initialize, container, agent, secrets, environment, lifecycle hooks, ssh config. `up` on a stopped container and `restart` use it; the CLI's `up` fast path is gone, and Up itself decides between no-op, resume, start, create and recreate.
- [x] Lifecycle markers: `/var/lib/dcx/lifecycle/<phase>.done` inside the container for onCreate, updateContent and postCreate, plus `ready` after the first postStart. A running container whose setup didn't finish is resumed (W2) instead of reported as running. Containers without the `lifecycle.markers` label (created by older dcx) are assumed complete, so upgrading never reruns create hooks.
- [x] `initializeCommand` runs on the host before the container is created or started, on every up that does work (W15). `postAttachCommand` runs on `dcx shell` (W16).
- [x] Typed lifecycle commands: `lifecycle.ParseCommand` is the single parser (config and feature hooks, names sorted); one execution path for parallel object-form commands; the unused `devcontainer.LifecycleCommand` is deleted.
- [x] Cancellation: Ctrl-C/SIGTERM cancel the CLI context. A cancelled hook or exec signals and closes its SSH session; a failing parallel command cancels its siblings (B5).
- [x] Agent: runs with output in `/tmp/dcx-agent.log`, and a failed start reports the log's tail. (Versioned atomic deploy and the readiness wait were done in Phase 1.)
- [x] `dcx build` builds exactly what `up` would run, including features and the UID layer (`UnifiedRuntime.buildImages`, H5).
- [x] Found while testing: lifecycle hooks inherited `os.Stdin`, and x/crypto/ssh keeps reading it after the session ends, so a hook swallowed the user's next keystrokes (e.g. the first input to `dcx shell`). Hooks now get an empty stdin.
- [ ] Not done: `waitFor` still only affects the "ready" message (everything runs in the foreground); `postAttachCommand` is not run for plain `ssh` sessions; image-metadata lifecycle commands are not run; `UnifiedRuntime` and the duplicate `DownOptions` remain (they fold into Phase 6's ContainerSpec work; DownOptions done in Phase 6).

### Phase 6: compose parity (3–4 days)

- [x] `container.ContainerSpec` holds every runtime setting of the primary container. `UnifiedRuntime.spec` is the only place it is derived from the resolved config. `dockerRunArgs` renders it for single-container plans, and `composeOverride` (yaml.v3 structs, not `fmt`) renders it for compose. This fixes W8: compose now gets capAdd, securityOpt, privileged, init, containerUser, feature security requirements, GPU (`deploy.resources`), appPorts, overrideCommand and typed mounts. Named volumes are declared with an explicit `name`, so they match what `docker run` uses. Values are `$`-escaped, so compose no longer re-interpolates resolved strings. Docker-only by design: container name, working directory, workspace mount (the compose file owns it) and runArgs.
- [x] One compose executor (`ComposeProject`, which replaces the `Compose` client and `runCompose`), always with explicit `-f` files. The override is persistent (`~/.dcx/compose/<workspace-id>/docker-compose.dcx.yml`, removed on `down`). Stop, start and down on an existing environment use the files and project directory that compose recorded on the container, skipping files that no longer exist. Build secrets go in a one-off override. Compose build and `config` use only the user's files.
- [x] `TestContainerSpecParity` checks, by reflection, that every `ContainerSpec` field is rendered by both engines or listed as docker-only. `TestSpecComposeParity` covers the W8 settings end to end through `spec`.
- [x] Also: the keep-alive entrypoint is a shell with a TERM trap (`docker stop` returns at once instead of after the 10s kill timeout). It ignores any service `command`, so `overrideCommand: true` works for compose. `runServices` always includes the primary service. `CreateContainer` now only creates (`docker create`). The duplicate `DownOptions` types are merged.
- [ ] Not done: `UnifiedRuntime` keeps its Start/Stop/Down methods next to the service's label-based paths. Folding those together belongs with the thin-CLI work in Phase 8.

### Phase 7: agent hardening (3–4 days)

- [x] Privilege drop, when the agent runs as root and the session user is not. Sessions keep the user's supplementary groups (they were dropped before, so for example docker or sudo membership was lost). The PTY device is chowned to the session user (sudo, ssh and gpg reopen `/dev/tty`). The forwarded SSH-agent socket gets a private directory handed to the session user and removed afterwards (before, it was root-only, so agent forwarding silently failed for a non-root `remoteUser`). SFTP already re-executed under the user's credentials.
- [x] Signals. The server forwards `signal` requests to the command's process group. When the client disconnects it sends SIGHUP, then SIGKILL after 5s (before, commands outlived their client). It reports `exit-signal`, so clients see 128+n. The client already sends SIGTERM and closes on Ctrl-C/cancel (Phase 5), which the server now acts on.
- [x] Nothing the root agent touches lives in `/tmp`. The binary is in `/var/lib/dcx/bin` (root-owned, installed with `install -d -o root`), the log in `/var/lib/dcx/agent.log`, and the shell integration and session config in `/run/dcx`. Since the listener runs as the container's default user (often not root), dcx first runs `dcx-agent setup` as root. It creates the root-owned directories, writes the scripts and hands the log file to the listener's uid. The old `/tmp/dcx-agent.log` let a container user make root truncate any file through a symlink. Agents still running from the legacy `/tmp/dcx-agent` are stopped on upgrade.
- [x] Host keys are never generated or replaced silently. The agent refuses to start without the key dcx wrote. The client and `dcx ssh info` only load keys (`LoadHostKey`), and so does SSH-config pinning. Only `up` creates one (`EnsureHostKey`), and a corrupt key is an error instead of being regenerated. There is no more `InsecureIgnoreHostKey` path. `dcx down` removes the workspace's host key; a recreate inside `up` keeps it.
- [x] Workspace blocks live in `~/.ssh/dcx/config`, pulled in by an `Include` at the top of `~/.ssh/config` (added once). Legacy blocks are moved out the next time a workspace's block is written. The user's file is otherwise left byte-for-byte untouched.
- [x] Plain `ssh` matches `dcx shell`/`exec` (details below).

#### Making ssh the only way in

Goal: `ssh <alias>` (terminal, editor, scp/sftp, git) gives the same session as `dcx shell`/`dcx exec`. Done in Phase 7:

- **Session environment.** `up` writes `agentproto.SessionConfig` to `/run/dcx/session.json`: the userEnvProbe result overlaid with remoteEnv (with `${containerEnv:…}` resolved). The agent applies it to every session, whatever the client. The client-sent env still comes last.
- **postAttachCommand.** Feature hooks come first, then devcontainer.json, converted with `lifecycle.AttachSteps` (same parser as every phase). The agent runs it at the start of each interactive session (PTY, no command), as the session user, with output on the terminal. A failure is reported and the shell still starts. `dcx shell` no longer runs it itself, so it runs exactly once per attach.
- **Alias collisions.** Every block answers to the unique `<container>.dcx`. It also answers to `<name>.dcx` unless another workspace's block already claims that alias; `up` then warns and prints the unique alias. `dcx ssh`, `dcx ssh info` and `dcx status` show the alias actually configured.
- **Still open, for Phase 8:** a friendly "not running" error (a thin `dcx ssh` launcher can preflight), and OpenSSH becomes a host requirement.

Lifecycle hooks still need an in-process client, to stream output and run as a specific user. `internal/ssh/exec` stays as a library for them; it is shrunk and renamed `internal/ssh/client` in Phase 8.

### Phase 8: thin CLI and cleanup (2–3 days)

- [x] `dcx shell` and `dcx exec` are deleted. `dcx ssh [-- cmd args…]` replaces them. It finds the workspace from the cwd, checks that the container is running and SSH is configured, then `syscall.Exec`s OpenSSH to the configured alias. It passes `-t` when stdin and stdout are terminals (`-T` otherwise) and joins the command with `shell.Join`, so `a "b c"` keeps its quoting. A command must follow `--`. `--connect` is kept as a hidden no-op. Printing the alias moved to `dcx ssh info`. The README has an "Upgrading" section with the breaking changes.
- [x] `dcx run` resolves the shortcut on the host and goes through the same launcher. Shortcuts are run with `sh -c '<text> "$@"' dcx-run <args…>`, so shell syntax in `command`/`prefix` works and arguments keep their exact value (before, `strings.Fields` split the text and quoted the pieces, which broke `&&`, pipes and quotes).
- [x] `internal/ssh/exec` became `internal/ssh/client`, which serves hooks only: `Run(ctx, RunOptions)`, non-interactive, with empty stdin, cancellable and with agent forwarding. TTY, window resize, interactive shell and the client-side remoteEnv/probe env are gone, because the agent gives every session `session.json`, hooks included. `HookRunner.SetProbedEnv` is gone too.
- [x] e2e: all 124 `dcx exec` calls run `dcx ssh --`, so the suite goes through OpenSSH and the generated config, the way users connect. Interactive tests run `dcx ssh` under a real PTY (`helpers.RunDCXInPTY`); a non-terminal `dcx ssh` correctly gets no PTY and no postAttach. New test: two same-name checkouts each reach their own container by their own alias, over plain ssh and `dcx ssh`.
- [x] `doctor` and `debug` resolve through `DevContainerService.Load`: same discovery (including `.devcontainer/<folder>/`), substitution, image metadata and lockfile as `up`. Before, they parsed the file themselves with no substitution, so they could report a different hash or user. `doctor`'s own `--config` flag shadowed the global `--config <path>`; it is now `--config-only`.
- [x] UI setup runs in `PersistentPreRunE` after Cobra parses flags; the early `ParseFlags` is gone.
- [x] `common`, `util` and the never-used `errors` package (`DCXError` was never constructed) are deleted. Their helpers moved to their owners: `devcontainer.SanitizeProjectName`/`SSHHostSuffix`, `container.ImageTagPrefix`, `features.DefaultHomeDir`, and private helpers elsewhere.
- [x] `.golangci.yml` adds `depguard` rules: no `internal/ui`/pterm outside `cli`/`ui`; the agent (`ssh/server`, `cmd/dcx-agent`) imports no Cobra or host-side packages; `agentproto` is stdlib-only. A probe confirmed the rules fire.
- [ ] Not done: a separate `app` layer. Commands already delegate to `DevContainerService`; the remaining direct `*Docker` use (`clean`, `list`, `logs`, stop/restart compose paths) is small. Doctor/debug results still render in the CLI, not through a `Diagnose` API. The friendly "not running" error exists only in `dcx ssh` (plain `ssh` reports connection refused).

**Total:** roughly 6–8 focused weeks for one person. Phases 0–1 deliver most of the risk reduction in the first week.

---

## E2E results

**Overall:** 103 e2e tests, all passing except 2 failures caused by network timeouts.

- `go test -tags=e2e -parallel=4` against local Docker 29.9.3 took 440s.
- `TestUserEnvProbeWithFeatureE2E` failed because apt couldn't reach the Debian mirrors inside the build.
- `TestComposeLocalFeatureCachingE2E` failed with a Docker Hub timeout resolving `docker/dockerfile:1.4`.

What the two failures still show:

- The generated feature Dockerfile pins a remote `# syntax=` frontend. That is an avoidable network dependency on every build.
- Every runtime error dumps the full `up` usage text (`SilenceUsage` is unset).
- The error message repeats "failed to build derived image" three times through wrapping.

**The suite passing doesn't contradict the findings above.** No e2e test runs `stop → up → exec` or `restart → exec`. A manual reproduction with the built binary confirms **W1**:

```
dcx up && dcx exec -- echo ok      # ok
dcx stop && dcx up                 # "Devcontainer started"
dcx exec -- echo ok                # ✗ ssh handshake: connection reset by peer  (+ full usage dump)
dcx restart && dcx exec -- echo ok # ✗ same
```

Phase 0 should add exactly these sequences as e2e tests.
