# dcx third audit (after the second audit's six steps)

**Question:** what still makes dcx less clean, correct or pleasant than it should be, now that the structural refactors are done?

**Short answer:** the architecture is no longer the problem. There's no god object and no leaky seams, the CLI is thin, only `container/docker.go` runs `docker`, and average cyclomatic complexity is 3.88. What remains falls into four groups:

1. **Bugs on common paths the e2e suite doesn't reach:**
   - compose with a non-root `remoteUser`
   - compose templates that use `network_mode: service:db`
   - Docker warnings on Apple Silicon
   - feature ordering with real (fully qualified) references
2. **A resolution pipeline that isn't one function.** Image-metadata merge, naming and the lockfile are assembled in the service, so golden tests skip half of it, and `dcx lock` has its own path. "Raw" vs "resolved" config is misnamed and inconsistently used.
3. **Untyped config.** `interface{}` lifecycle commands, ports and GPU requirements force type switches in several places. They also block two spec features: image-metadata lifecycle hooks and feature entrypoints.
4. **Tests and docs that can't be fully trusted:**
   - four e2e checks can never fail;
   - e2e touches the developer's real `~/.ssh` and Docker images;
   - about a quarter of e2e is duplicated;
   - `Up` has no unit tests;
   - a clean checkout doesn't compile;
   - parts of CLAUDE.md and the README describe old behaviour.

None of this needs re-architecture. It's six focused steps (section 6).

## How this was produced

- **Measured:** package sizes, test coverage, gocyclo.
- **Reviewed independently:** three reviews, covering (a) the domain model and resolution pipeline, (b) correctness and robustness, and (c) tests, developer experience, CLI UX and docs.
- **Checked:** each "confirmed" finding below was checked against the code; the high-severity ones were also checked by hand while writing this. "Plausible" means the code path is clear but depends on Docker behaviour that wasn't exercised.

| Package | Lines | Coverage | Hotspots (gocyclo) |
|---|---|---|---|
| devcontainer | 3,047 | 72.5% | `ParseRunArgs` 29 and `ParsePortSpec` 22 (flat switch tables, fine), `Builder.Build` 28 |
| cli | 2,802 | 20.6% | `renderStatus` 23, `runDoctor` 23 |
| container | 2,352 | 35.3% | `dockerRunArgs` 21 (flat) |
| features | 2,275 | 45.0% | `fetchOCIWithDigest` 29, `extractTarReader` 24 |
| agent | 1,356 | 49.2% | |
| service | 1,339 | 29.5% | `Lock` 23, `setupContainerEnvironment` 18; `Up` and its steps at 0% |

## 1. Bugs

### High

| # | Where | Problem | Status |
|---|---|---|---|
| R1 | `container/docker.go:370` | `CreateContainer` uses `CombinedOutput()` as the container ID. Any Docker warning (platform mismatch on Apple Silicon, "published ports are discarded", memory-limit notices) corrupts the ID, so `start` fails. The container is left Created, so the next `up` "works" and the failure looks random. | confirmed |
| R2 | `container/provisioner.go:103` | Compose `up --build` runs whenever there are no features, even when dcx layered a UID image (`ShouldUpdateUID`). Compose then rebuilds the user's service and tags it with dcx's `uid-*` tag, which drops the UID remap. This hits every recreate or `--rebuild` for compose + `build:` + non-root `remoteUser`. | confirmed |
| R3 | `container/provisioner.go:161` | With compose and layering, `compose build` skips image-only services and nothing pulls them, so `ImageID` on the base fails on a clean machine. e2e misses it because `alpine` is cached. | confirmed |
| R4 | `service/up.go:219` | A recreate removes the old environment before building the new images. A Dockerfile typo, a registry outage or a failing feature leaves the user with nothing running. | confirmed |
| R5 | `container/provisioner.go:575`, `compose_override.go:151` | The agent's SSH port is always published. Docker rejects published ports with `network_mode: service:x`/`container:` (the "app + Postgres" templates), and ignores them with `host`, where `PortMapping` then fails, so every hook and the SSH config fail. | plausible |
| D1 | `features/ordering.go:19-100` | The dependency graph is keyed by metadata ID (`common-utils`), but `dependsOn`/`installsAfter`/`overrideFeatureInstallOrder` use full references (`ghcr.io/…/common-utils:2`), so the edges never match. Order then falls back to map-iteration order, which is random between runs. The tests pass only because they use short IDs. | confirmed |
| D2 | `features/manager.go:239` | `installsAfter` queues features for installation. Per the spec it is only an ordering hint. | confirmed |
| D3 | `devcontainer/plan.go:110`, `builder.go:103`, `provisioner.go:255` | `runServices` is never copied into the compose plan, so all services always start. `build.cacheFrom` is set on the plan but never passed to the build. The golden view omits both fields. | confirmed |
| U1 | `cli/root.go` | A relative `-w` is never made absolute, so `${localWorkspaceFolder}`, the `workspace.path`/`config.path` labels and bind sources come out relative. | confirmed |

### Medium

| # | Where | Problem |
|---|---|---|
| R6 | `agent/session.go:135` | If `user.Lookup` fails (numeric `remoteUser: "1000"`, or a user missing from `/etc/passwd`), `sessionCredential` returns nil and sessions, SFTP and hooks run as the agent, usually root. It should try `LookupId` and refuse otherwise. |
| R7 | `service/up.go:72,132` | "Already running" doesn't check the agent. After an external `docker restart` or a reboot with a restart policy, the ready marker persists but the agent and `/run/secrets` are gone. `up` says "already running" and changes nothing. |
| R8 | `agentctl/provision.go:85,140` | The host key is chowned to `remoteUser` (mode 0400), but the agent runs as the container's default user. When those differ (`USER app`, `remoteUser: root`), the agent can't read the key. Launch the agent as root (it drops privileges per session) or chown to the uid from `ownerQuery`. (plausible) |
| R9 | `provisioner.go:604`, `ssh/ports.go` | The deterministic SSH port can collide with a stopped workspace's port. A Created container then fails `start` forever with "port already allocated", and there's no fallback. |
| R10 | `cli/up.go:55`, `service/stop.go:87` | `--hosts` only applies at create time and isn't stored. `restart` relaunches the agent loopback-only while the port stays published on 0.0.0.0. |
| R11 | `devcontainer.go:449`, `up.go:226` | Recreate runs `compose down` without `--remove-orphans` against the edited files. A renamed primary service survives, giving two "primary" containers, and `State` picks one arbitrarily. |
| R12 | `agent/session.go:289` | A PTY session hangs after `exit` while a background job holds the TTY (`io.Copy` waits for every slave file descriptor to close). |
| R13 | `env/probe.go:167` | `userEnvProbe` runs `sh` (dash on Debian), not the user's login shell, so nvm, rbenv and asdf PATH entries are never captured. |
| D4 | `features/types.go:351`, `hashes.go:116` | Feature options get a second, partial substitution (only `localEnv`) at Dockerfile time; `${devcontainerId}` and the others are never expanded there, and the staleness hash uses the unsubstituted options. This breaks "substitute once". |
| D5 | `builder.go:380`, `provisioner.go:385` | `hostRequirements.gpu: "optional"` (and `{}`) adds `--gpus all`, which fails on hosts without a GPU. `hostreqs.go` interprets the same value differently. |
| D6 | `provisioner.go:253,300` | The image-metadata-merged config is passed as `LocalConfig` to the feature and Dockerfile builds next to `BaseImageMetadata`, so base-image metadata appears twice in the derived image's label. |

### Low (hardening)

| # | Problem |
|---|---|
| R14 | `/etc/environment`: any `cat` failure is treated as "missing", so a transient error drops existing lines. |
| R15 | `/etc/environment`: values containing `"` are written unescaped. |
| R16 | Lifecycle markers fail open: an exec error counts as "not done", so `onCreateCommand` re-runs. |
| R17 | `parseEnvOutput` splits multi-line values (bash functions) into bogus keys. |
| R18 | `ImageID`, `ExecOutput(As)`, `ComposeOutput` and `ListContainersWithLabels` drop Docker's stderr from errors. |
| R19 | `hostconfig.renderBlock` writes `remoteUser` unvalidated into the SSH config. |
| R20 | No per-workspace lock against concurrent `dcx up`; `EnsureHostKey` uses a fixed `.tmp` name. |
| R21 | `/run/secrets` is a 1 MiB tmpfs; large runtime secrets fail opaquely. |
| D7 | `Validate` runs only in `dcx config`, so `up` silently picks compose over build over image when several are set. |
| D8 | The `userEnvProbe` default should be `loginInteractiveShell` (the spec); dcx treats empty as none. |
| D9 | OCI feature cache entries are keyed by tag, so a mutable `:1` stays cached forever unless `--pull` is given. |

## 2. Resolution pipeline (structure)

- **The pipeline is assembled in the service** (`LoadWithOptions`): load, image-metadata merge (engine I/O), name sanitising, lockfile, `Builder.Build`. The golden tests call `Builder.Build` directly and skip half of it; for example, they show `name: "Compose App"` where production gives `compose_app`. `Service.Lock` is a second pipeline. **Fix:** `devcontainer.ResolveWorkspace(ctx, Inputs, ImageMetadataSource)`, where the source is a one-method interface the service implements with the engine. Golden tests and `Lock` go through it.
- **Identity is computed in three disagreeing places:** `ComputeIdentity` (raw name), `LoadWithOptions` (sanitised), and `Builder.Build` (compose `name:` overrides). For compose projects with an explicit name, `Identity().ResourceName`, which stop and down fall back to, differs from the container labels. **Fix:** compute `Identity` once inside resolution and store it on `ResolvedDevContainer`.
- **Raw vs resolved:** `RawConfig` is documented as the parsed file but is actually the merged config. Consumers still read it for `OverrideCommand`, `HostRequirements`, `PostAttachCommand`, the lifecycle commands and `UserEnvProbe`. Some resolved fields are dead (`Customizations`, `DockerfilePlan.BaseImage`, `ComposePlan.WorkDir`, `PortForward.Label`/`OnAutoForward`). **Fix:**
  - rename `RawConfig` to `Config` (merged) and keep `Local` (pre-merge);
  - add the few resolved fields consumers need;
  - delete the dead fields.

  Then nothing outside `devcontainer` reads the config except display.
- **Feature runtime requirements are merged twice** (builder and provisioner). Mounts are parsed three times. `FeatureMount` has its own string parser, against the CLAUDE.md rule.

## 3. Typed config

- **Lifecycle commands** are `interface{}` in `DevContainerConfig` and `FeatureMetadata`. Every consumer calls `ParseCommand`, and `substituteAny` exists mostly for them. Image-metadata lifecycle hooks are dropped on purpose (`metadata.go:38`); the spec runs them in order: image, then features, then local. **Fix:**
  - a `LifecycleCommand` type with an unmarshaler;
  - `Build` resolves per-phase `[]HookStep{Origin, Command}` lists;
  - `HookRunner` takes those lists.

  This deletes the five `Collect*Commands` wrappers and `SetFeatureHooks`, and makes image hooks a single append.
- **Feature `entrypoint` is ignored** (`types.go:126`), so docker-in-docker and others never run their init. **Fix:** chain feature entrypoints in front of the keep-alive.
- **Ports:** `ForwardPorts []interface{}`, `AppPort interface{}` and `PortsAttributes map[string]interface{}` should become a `PortList` type and a typed attributes map. That deletes `portValues`, `unionInterfaces` and the type switch in `PortAttributeFor`.
- **GPU:** `GPU interface{}` should become a typed `GPURequirement{Mode, Count, …}` read by both sites. GPU detection (`nvidia-smi`) should move out of the domain package.

## 4. Tests and developer experience

- **T1 (High), always-passing checks:**
  - `TestEnvProbeCachingE2E` checks a cache that no longer exists, using `Contains("cached")`, which "not-cached" also matches.
  - `user_env_probe_e2e_test.go:103` has the same patched/not-patched pattern.
  - `ssh_agent_e2e_test.go:214` prints the success word when the agent is unreachable.
  - `TestSSHServerCleanupE2E` looks for an `ssh-server` process that no longer exists.
- **T2 (High), real environment:** e2e uses the developer's real `~/.ssh` and Docker images. `TestCleanRemovesUnusedDcxImagesE2E` runs a global `dcx clean`, and every run leaves `dcx/<ws>:*` images behind. **Fix:** a `DCX_SSH_DIR` override plus `ssh -F`, a scoped clean, and image teardown in `TestMain`.
- **T3 (High), clean checkout:** a clean checkout doesn't compile, because `agent-embed.go` embeds the gitignored `bin/*.gz`. **Fix:** commit empty placeholders (`Binary` handles empty input) or add a generate step, and document it.
- **T4, human output:** tests parse human `status` output ("State:", "Name:", "Project:") and log text. **Fix:** `status --json` and `list --json` from `StatusReport`/`Environment`, and move the helpers onto them.
- **T5, timing thresholds:** these will flake on loaded CI runners: second deploy under 2s, stop under 8s, hooks within 1s, `Sleep(500ms)`. Assert the behaviour instead.
- **T6, duplication:** about a quarter of e2e is duplicated (override-command ×2, env ×3, same-name ×2, `/etc/environment` ×2, the secrets matrix, …). `error_handling_e2e` needs no container. Merge into table subtests that share one container per config.
- **T7, helpers:**
  - helpers are copy-pasted across e2e files;
  - `GetDCXBinary` silently reuses a stale binary;
  - `RequireDockerAvailable` skips (green) when Docker is missing in CI.

  **Fix:** one `helpers.Workspace` type, a `TestMain` that fails when `CI` is set, and a binary helper that says "run make build".
- **T8, `Up` coverage:** `Up` and its steps have 0% unit coverage. Inject the agent provisioner and the SSH-config writer, then table-test `Up` against `containertest.Fake`: create, resume after a failed hook, recreate on staleness, start running postStart only.
- **T9, low-value tests:** flag-existence tests remain in `cli/commands_test.go`. Replace them with golden `--help` output.
- **Makefile and CI:**
  - `deadcode@latest` is unpinned and probably exits 0, so it can't fail CI;
  - `install` uses an unset `$(GOPATH)`;
  - `deps` runs `go mod tidy`;
  - several targets are missing from `.PHONY`;
  - `release.yml` runs no e2e.

## 5. CLI UX and docs

- **`--quiet`** drops data output (`dcx -q run --list` prints nothing). Quiet should suppress progress only.
- **Colour:** always on, even when piped. `NO_COLOR` is ignored. `lock` prints the same error twice, plus spinner `\r` junk off a TTY.
- **Error messages:** "docker not accessible: exit status 1" hides Docker's stderr. `status` without a config reports the Docker error first.
- **Overlapping commands:** `status --detailed`, the hidden `debug`, `config`, `doctor` and `ssh info --doctor` overlap. `--json` exists only on `debug` and `ssh info`. JSON keys mix `snake_case` and `camelCase`. `plan_type: "single"` contradicts the image/dockerfile/compose vocabulary.
- **Help text:**
  - `debug` advertises a non-existent `-p`;
  - `--pull` doesn't say it also re-pulls the base image;
  - `lock --update` equals no flag.
- **CLAUDE.md:** the "Container Lifecycle (Up)" data flow still names functions that don't exist (`ApplyUIDUpdate`, `RunLifecycleHooks`, …). It should list `env`, `secrets`, `lockfile` and `agentctl`, and be checked against `ls internal/*`.
- **README:**
  - "Project Naming" is wrong (the compose `name` isn't used; with no `name` the alias is `<id>.dcx`);
  - the `postStartCommand: "npm run dev"` example would hang `up`, since hooks run in the foreground;
  - it names a `make build-release` target that doesn't exist;
  - several flags are undocumented.
- **docs/:** `audit-and-refactor-plan.md` should carry a "historical; superseded" banner.

## Status

**Step 1 (common-path bugs): done**, each with a regression test:
- **R1:** `CreateContainer` reads the ID from stdout only (the last line). Warnings stay on stderr, and stderr is used in errors.
- **R2:** compose `up --build` runs only when dcx didn't layer the image.
- **R3:** an image-only compose base is pulled (`ensureImage`) before dcx layers on it.
- **R4:** a recreate builds first (`Provisioner.BuildImages`, which also runs `compose build` for unlayered compose) and removes the old environment only after the build succeeds; the error says the old one was left running.
- **R6:** the agent looks the session user up by name, then by ID. A numeric UID without a passwd entry runs as that UID. An unresolvable user refuses the session and SFTP instead of running them as root.
- **R7:** "already running" also requires the agent to answer. Otherwise `up` resumes and says the agent wasn't running.
- **D1:** feature ordering uses one canonical key and maps every `dependsOn`, `installsAfter` and override reference through `normalizeFeatureRef`. Features not listed in the override follow in deterministic dependency order.
- **D2:** `installsAfter` no longer installs features.
- **D3:** `runServices` reaches the compose plan and `cacheFrom` reaches the build. The golden plan view now includes `runServices`, `cacheFrom` and `options`.
- **D5:** `gpu: "optional"` adds `--gpus` only when the Docker host has the NVIDIA runtime (`SystemInfo.GPU`); an empty GPU object means no requirement.
- **D6:** image metadata labels use the pre-merge config (`ResolvedDevContainer.LocalConfig`).
- **U1:** `-w` is made absolute in the root command.

Regression tests cover:
- the compose layering cases (confirmed failing on the old code);
- feature ordering with full references and `installsAfter`;
- session-user resolution;
- optional GPUs;
- `lastLine`;
- `BuildImages` for compose;
- absolute `-w`.

**Step 2 (test trust and hygiene): done.**
- **T1:** `TestEnvProbeCachingE2E` is deleted (it tested a removed cache). The patched/not-patched check uses `Equal`. The SSH agent check asserts `ssh-add`'s exit code (0 or 1 means it reached the agent; 2 means it didn't). The agent cleanup test counts the agent's child processes after sessions end.
- **T3:** `agent-embed.go` embeds the committed `agentbin/` directory (a README plus the generated `.gz` files), so a clean checkout builds, vets and tests. `Binary()` says to run `make build` when the binaries are missing.
- **T7:**
  - Docker is checked once, and `CI=1` turns a missing daemon into a failure instead of skipped (green) tests.
  - `GetDCXBinary` refuses a missing `bin/dcx`, or one older than the sources, instead of silently building one without agents.
- **T2:**
  - `DCX_SSH_DIR` relocates everything dcx writes for SSH (main config, include file, the `Include` line). `dcx ssh` passes `-F` when it's set.
  - e2e `TestMain` sets it to a temp directory, and every OpenSSH call goes through `helpers.SSHCommand` or `helpers.SFTPCommand`.
  - `TestMain` removes the `dcx/<test workspace id>:*` images afterwards.
  - The clean test uses the new `dcx clean --current`, which only touches the current workspace's images.
- **T4:** `dcx status --json` and `dcx list --json` have stable camelCase keys, with golden tests. The e2e helpers (`GetStatus`, `GetContainerState`, `ContainerName`) use them, and all status-text parsing is gone from e2e.
- **T5:**
  - The parallel-hooks test uses a rendezvous: each hook waits for the others, so it fails rather than flakes when run in sequence.
  - The fast-stop test checks exit code 0 (not 137).
  - The deploy test asserts no reinstall instead of a time limit.
- **T6:** five duplicate tests are removed (override-command false, two containerEnv tests, same-name coexist, `/etc/environment` patching, merged into the "follows config" test). `TestExec*` is renamed to `TestSSHCommand*`/`TestSSH*`. There are now 121 e2e tests. The secrets matrix and `error_handling_e2e` are left as they are.
- **Makefile:**
  - `deadcode` is pinned and fails when dead code is found (checked with a probe file);
  - `install` uses `go env GOPATH`;
  - `deps` no longer runs `go mod tidy` (new `tidy` target);
  - every target is in `.PHONY`.

**Step 3 (agent reachability): done.** The agent's endpoint is decided when the container is created and recorded in labels (`ssh.port`, `ssh.listen`, `ssh.allow`). Hooks, `ssh info` and the SSH config read the port from there; containers made by older dcx versions fall back to asking Docker.
- **R5:** the network mode decides how the agent is reached:
  - default/bridge/user networks publish the port on the container;
  - `host` publishes nothing, and the agent listens on `127.0.0.1:<workspace port>` (or `0.0.0.0` with `--hosts`);
  - compose `network_mode: service:<x>` publishes on `x`;
  - `container:<x>` and `none` fail with an explanation, before a recreate removes anything.
- **R8:** the agent runs as root and switches to the session user for each session. The SSH secrets are root-owned, `dcx-agent setup --log-owner` is gone, and without a `remoteUser`/`containerUser` sessions run as the container's default uid.
- **R9:** `PickPort` starts at the workspace's preferred port and probes the next ones. It skips ports that are bound or recorded on another workspace's container (stopped or not), so there's no ephemeral fallback any more. A start that fails because a published port was taken explains how to recover (`dcx up --recreate`).
- **R10:** `--hosts` is recorded on the container. `up`, `restart` and `--recreate` without the flag keep it; a different `--hosts` without a (re)create fails and names `--recreate`.
- **R11:** a recreate runs `compose down --remove-orphans`, and every `down` leaves out dcx's own override. Written for the old config, that override made compose reject the project once the primary service was renamed. Named volumes of `mounts` are therefore no longer removed by `down -v`, matching single containers.

Regression tests: unit tests for the endpoint (published/recorded, port collision, widened access, host, `container:`, compose owner), `PickPort`, `sshAccessFor`, the agent's listen address and default user, and `ForRemoval`. New e2e tests cover host networking, compose `service:` networking, a renamed compose service, root `remoteUser` on a non-root image, and `--hosts` across restart and recreate (126 e2e tests).

**Step 4 (one resolution function): done.**
- **`devcontainer.ResolveWorkspace`** is the only pipeline: load and substitute, merge image metadata, lockfile, build.
  - Image metadata comes in through a one-method `ImageMetadataSource`, which the service implements with the engine, pulling the image when asked.
  - `LoadWithOptions`, `Lock`, status, debug and the golden tests all go through it. The service's own `mergeImageMetadata`, project-name sanitising and `Lock`'s separate feature manager are gone.
- **Identity:** `NewIdentity` is the only place it is computed, and `ResolvedDevContainer` embeds it.
  - The name is devcontainer.json's `name` (or the directory name), and the SSH alias comes from it. The resource name is `<slug>-<id8>`, except that a compose top-level `name:` sets the compose project.
  - Commands that don't resolve use `LoadIdentity`, and the golden test asserts it equals the resolved identity in every case.
  - `ComputeIdentity`, `ComputeName` and `BuilderOptions.ProjectName` are deleted.
  - Visible changes: labels and status show the display name (`Compose App`, not `compose_app`), and a compose `name:` no longer renames the workspace. `DCX_PROJECT_NAME` stays sanitized.
- **Config fields:**
  - `RawConfig` is renamed `Config` (merged) and `LocalConfig` is renamed `Local` (as written).
  - New resolved fields: `OverrideCommand` (spec default applied in the builder), `HostRequirements` and `UserEnvProbe`. Their readers moved off the config.
  - Lifecycle commands and `postAttachCommand` still read `Config` until step 5 types them.
- **Deleted:** `Customizations`, `DockerfilePlan.BaseImage`, `ComposePlan.WorkDir`, `PortForward.Label`/`OnAutoForward`, `BuilderOptions.SubstitutionContext` and the provisioner's `overrideCommand`/`localConfig`.
- **Golden tests** now run `ResolveWorkspace`, with new `image-metadata` and `compose-named` cases and `resourceName`/`sshHost`/`overrideCommand` in the view.
- **T8:** `Up` has unit tests against `containertest.Fake`, with seams for the agent provisioner and the lifecycle hooks:
  - create, then a no-op second run;
  - resume after a failed hook;
  - start running postStart again;
  - recreate on staleness;
  - a pre-marker container whose agent is down.

  The last one found a panic: a nil `*containerMarkers` was passed as a non-nil `lifecycle.Markers`, and the hook runner dereferenced it. That is fixed, and confirmed by running the test against the old code. The fake now gives unique container IDs.

## 6. Plan

Each step ships on its own and is a few hundred lines.

1. **Common-path bugs, each with a regression test.** R1–R4, R6, R7, D1–D3, D5, D6 and U1. They're small local changes, and several need only a `containertest.Fake` test.
2. **Test trust and hygiene.** T1 (fix or delete the four checks), T3 (clean checkout builds), T7 (fail rather than skip in CI; one `Workspace` helper), T2 (`DCX_SSH_DIR`, scoped clean, image teardown), T4 (`status`/`list --json`, helpers on JSON), T5 (no timing thresholds), and T6 (merge duplicates). Pin `deadcode` so it can fail CI.
3. **Agent reachability.** R5 (network modes: no publish, then reach the agent via the network owner or a docker-exec hook transport, or `127.0.0.1:48022` for host), R8, R9, R10, R11.
4. **One resolution function.** `devcontainer.ResolveWorkspace` with an `ImageMetadataSource`; `Identity` computed there; `RawConfig` renamed to `Config`/`Local`; resolved fields for what consumers read; dead fields deleted; golden tests and `Lock` through it. Then T8 (`Up` unit tests).
5. **Typed config.** `LifecycleCommand` and per-phase hook lists, with image-metadata hooks and feature entrypoints on top of them. Then `PortList`/`PortAttribute`, `GPURequirement` and the single mount parser. D4 (substitute feature options once).
6. **UX and docs.** Quiet/colour/`NO_COLOR`, errors with stderr (R18), `--json` consistency, folding `debug` into `status --detailed --json`, help fixes, the CLAUDE.md and README rewrites, and the docs banners. Then the R12–R21 and D7–D9 hardening items.

Image metadata for Dockerfile and compose bases (a two-phase resolve) and `waitFor` come after step 5, when the merge feeds typed, ordered lists.
