# dcx fourth audit (after the third audit's six steps)

**Question:** what still makes dcx less correct, clean or pleasant than it should be, now that three rounds of refactoring are done?

**Short answer:** the structure is in good shape, and nothing calls for re-architecture. The layering holds, `ResolveWorkspace` and `NewIdentity` are the single sources they claim to be, `ContainerSpec` with its two renderers is a good seam, and the typed config round-trips under the hash pin. Deadcode is clean even without `-test`. What remains is mostly **behaviour at the edges of the devcontainer spec** that no test reached:

1. **Features and ports, on common configs.** Features install as the image's user. `appPort` can't work at all. `forwardPorts` is published on every interface.
2. **Image metadata, done halfway.** It is merged raw (no substitution), its entrypoints are dropped, and it is still not read for Dockerfile or compose bases.
3. **Silent recreates.** Volatile inputs (`${localEnv:…}` in mounts, a moved feature tag) make an unchanged config stale, and stale recreates without asking.
4. **Leftover seams.** CLI commands still build engines, the UID decision lives in three places, feature requirements are merged twice, and SELinux support is claimed but not implemented.
5. **Tests that leak and gaps that matter.** e2e still writes to the real `~/.dcx` (777 host keys and 122 lock files on this machine). Feature fetching, registry auth and lockfile verification have no unit tests. Releases aren't gated on CI.

## How this was produced

- **Measured:** package sizes, test coverage, gocyclo (average 3.73).
- **Reviewed independently:** three read-only reviews, covering (a) correctness, robustness and security, (b) structure, domain model and spec conformance, and (c) tests, developer experience, CLI UX and docs. Each read the earlier audits so it would not re-report fixed items.
- **Checked:** the High findings were checked by hand against the code. `appPort` was also checked against Docker (`docker run -p localhost:19999:80` fails with "invalid IP address: localhost"). "Plausible" means the code path is clear but the outcome depends on Docker, OpenSSH or registry behaviour that wasn't exercised.

| Package | Lines | Coverage | Note |
|---|---|---|---|
| devcontainer | 3,181 | 81.3% | `Builder.Build` 24 |
| container | 2,814 | 50.8% | `dockerRunArgs` 21 (flat) |
| cli | 2,570 | 26.7% | `renderStatus` 24, `runDoctor` 23; every `run*` at 0% |
| features | 2,103 | 48.1% | fetch, auth, integrity and lockfile verify at 0% |
| service | 1,457 | 60.5% | `Up` 24 |
| agent | 1,414 | 54.3% | |

18.7k lines of production Go and 17.9k of tests; 127 e2e tests (about 140s warm).

## 1. Correctness and security

### High

| # | Where | Problem | Status |
|---|---|---|---|
| F1 | `features/dockerfile.go:60-100` | The features Dockerfile goes `FROM <base>` straight into `RUN install.sh` with no `USER root`, and never restores the image user. Any base whose final `USER` is non-root (a Dockerfile plan ending in `USER vscode`, `jupyter/*`) runs feature scripts unprivileged, so `apt-get` fails and so does `up`. No e2e test combines a non-root `USER` with features. | confirmed |
| F2 | `container/provisioner.go:605-629` | **Ports are inverted from the spec.** `appPort` gets host `"localhost"`, which Docker rejects ("invalid IP address: localhost"), so every config with `appPort` fails create. `forwardPorts` is published as `3000:3000` on every interface: dev servers are reachable from the LAN, two workspaces with the same `forwardPorts` collide, and with compose `network_mode: service:x` the ports still land on the primary service, which Docker rejects. The comment says this is "per the devcontainer spec"; it is the reverse. | confirmed |
| F3 | `devcontainer/builder.go:173`, `metadata.go:176-188`, `secrets/secrets.go:95` | **A base image can run commands on the host.** `customizations.dcx.secrets` and `buildSecrets` are read from the merged config, and `mergeCustomizations` copies a whole tool block from an image layer when the local config has none. Secrets run as `sh -c` on the host, so an image whose `devcontainer.metadata` carries `dcx.secrets` gets host command execution on `dcx up`. `initializeCommand` is already local-only; shortcuts are read locally by `run` but counted from the merged config by `status`. | confirmed |
| F4 | `devcontainer/hashes.go:35-118`, `service/up.go:176` | **An unchanged config can go stale, and stale recreates without asking.** The hash covers the substituted config and each feature's manifest digest. `${localEnv:SSH_AUTH_SOCK}` in a mount changes with every login. A tag reference such as `…/node:1` without a lockfile is re-checked after 24h, and its digest changes when upstream releases. Either way the next `up` silently recreates the container, losing everything outside volumes. | confirmed |

### Medium

| # | Where | Problem | Status |
|---|---|---|---|
| F5 | `container/provisioner.go:366-370`, `build/uid.go:53` | The UID layer sets `USER` to containerUser, falling back to remoteUser. The reference implementation keeps the image's own `Config.User`. With an mcr image (USER root, metadata `remoteUser: vscode`) on Linux, the keep-alive, feature entrypoints (dockerd init) and compose commands then run as vscode. | confirmed; breakage plausible |
| F6 | `devcontainer/resolve.go:64-65`, `metadata.go:40-113` | Image metadata is merged raw: `SubstituteConfig` only runs on the local file, so a prebuilt docker-in-docker image's `source=dind-var-lib-docker-${devcontainerId}` reaches Docker literally. `DevContainerConfig` has no `entrypoint` (or `id`), so entrypoints recorded in an image's label are dropped and dockerd never starts. dcx's own label omits them for features too. | confirmed |
| F7 | `service/up.go:123-131` | A failing hook leaves the workspace with no SSH entry. Hooks run before the SSH config step, and a recreate has already removed the old block. After a failed `postCreateCommand`, `dcx ssh` says "SSH is not configured", so the user can't get in to debug it. | confirmed |
| F8 | `devcontainer/id.go:37-60` | Multiple configs in one workspace (`.devcontainer/a`, `.devcontainer/b`, selected with `--config`) share one workspace ID and `${devcontainerId}`. Switching configs recreates the other's container. The spec derives the ID from the folder and the config file. | confirmed |
| F9 | `devcontainer/id.go:83-87` | A top-level compose `name:` makes every checkout (worktrees, clones) share one compose project, so they replace each other's containers and SSH blocks. | confirmed; compose takeover plausible |
| F10 | `ssh/hostconfig/config.go:257,270` | `IdentityFile` and `UserKnownHostsFile` are written unquoted. With a home directory containing a space, OpenSSH rejects the line, and because dcx's `Include` is at the top of `~/.ssh/config`, every `ssh` (and git over SSH) fails. | confirmed; error text plausible |
| F11 | `container/provisioner.go:284-321` | Dockerfile builds never pass `--no-cache` or `--pull`: `DockerfileBuildOptions.NoCache`/`Pull` are never set. `dcx build --no-cache` only skips dcx's own tag reuse, and `up --pull`/`build --pull` don't refresh the Dockerfile's `FROM`, despite the help text. | confirmed |
| F12 | `features/types.go:211-235` | Digest-pinned feature references (`…/node@sha256:<hex>`) parse into resource `node@sha256` and version `<hex>`, so the manifest URL and token scope are wrong and the fetch fails. The resolver's `@sha256` suffix check works around the symptom. | confirmed |
| F13 | `features/manager.go:71-82` | A feature given as a string (`"…/node:1": "18"`) gets empty options, so the version is silently ignored. The reference implementation treats it as the `version` option. | confirmed |
| F14 | `service/up.go:100-101,433` | After a restart outside dcx (`docker restart`, a reboot with a restart policy), `up` reconnects the agent but skips `postStartCommand`: the action is resume, `started` is false and the ready marker exists. | confirmed |
| F15 | `service/stop.go:81-87`, `cli/down.go` | `down`, `stop` and `restart` don't take the workspace lock. A `dcx down` during an `up` can remove the container and host key mid-provision. `Restart` stops before `Up` locks. | confirmed |

### Low (hardening)

| # | Problem |
|---|---|
| F16 | `formatMount` doesn't CSV-quote, so a path containing a comma breaks `--mount`. |
| F17 | The fallback client key pair (`~/.dcx/id_ed25519{,.pub}`) is written non-atomically without a lock; two workspaces running `up` at once can leave a mismatched pair that the fast path keeps forever. |
| F18 | `dcx down --volumes` ignores the flag for single containers (named volumes from `mounts` survive). |
| F19 | Agent liveness is a TCP connect; with host networking an unrelated process on the port reads as a running agent. (plausible) |
| F20 | Mount options `bind-propagation`, `volume-opt`, `volume-subpath`, `tmpfs-size`/`tmpfs-mode` are dropped; relative short-form bind sources (`./data:/data`) reach Docker unresolved. |
| F21 | With widened `--hosts`, the container's default gateway is always allowed; where Docker proxies published ports (Docker Desktop, rootless), every client appears as the gateway and the CIDR gate passes everyone. Key auth remains the control. (plausible) |

## 2. Spec conformance

| # | Where | Problem | Severity |
|---|---|---|---|
| S1 | `devcontainer/resolve.go:90-107` | Image metadata is still not read for Dockerfile or compose bases (deferred by the third audit). `FROM …/typescript-node` loses `remoteUser: node`: sessions and hooks run as root, the UID layer is skipped and workspace files end up root-owned. Needs the two-phase resolve. | High |
| S2 | `lifecycle/hooks.go:88-92,134-139` | `waitFor` is cosmetic: it only decides when "Container is ready (remaining hooks running...)" is printed; every phase still blocks `up`. | Medium |
| S3 | `devcontainer/hostreqs.go:35-58`, `metadata.go` | `hostRequirements` cpus/memory are a hard gate, so repos with Codespaces-sized minimums can't open on laptops; image-metadata values aren't merged (the spec takes the max); `storage` warns on every `up`. | Medium |
| S4 | `features/resolver.go:771-845` | Private feature registries are unsupported: token requests are always anonymous (no `~/.docker/config.json`, credential helpers or `GITHUB_TOKEN`). | Medium |
| S5 | `features/dockerfile.go:272-277`, `build/features.go:132-145` | `_REMOTE_USER_HOME`/`_CONTAINER_USER_HOME` are guessed as `/home/<user>`; comments claim getent resolution that doesn't exist. | Medium |
| S6 | `build/uid.go:140-157` | `updateRemoteUserUID` is applied on macOS; the spec and reference CLI apply it on Linux only (and GID 20 collides with `dialout` on Debian). | Medium |
| S7 | `config.go`, `validate.go:62-65` | The legacy top-level `dockerFile`/`context` are not recognised; older configs fail validation. | Medium |
| S8 | `features/types.go:95-99` | Feature `legacyIds` and `deprecated` are parsed and ignored (no warning; legacy IDs don't match dependency references). | Low |
| S9 | `metadata.go:11-22` | A single-object `devcontainer.metadata` label is rejected; the reference implementation accepts it. | Low |
| S10 | `metadata.go:49-104` | `otherPortsAttributes` isn't merged from image layers; port attributes otherwise only reach the label. | Low |
| S11 | `service/stop.go:92-96` | `shutdownAction` is read from the local file only, `stopContainer` vs `stopCompose` isn't distinguished, and `none` turns an explicit `dcx restart` into a silent no-op. | Low |
| S12 | `builder.go:86`, `validate.go:99` | `build` with only `context` passes the config directory as the Dockerfile. | Low |
| S13 | `config.go:53` | `remoteEnv: {"X": null}` becomes `X=""` instead of unsetting X. | Low |
| S14 | `parser.go:90-95` | Config discovery falls back to the first `.devcontainer/*.json`, which can pick an unrelated file. | Low |

## 3. Structure

| # | Where | Problem | Severity |
|---|---|---|---|
| Q1 | `devcontainer/builder.go:246-278`, `container/provisioner.go:511-531` | Feature `capAdd`/`securityOpt`/`privileged`/`init` are merged in the builder and again in `applyFeatureRequirements`, so each is emitted twice (raised in the third audit's §2; only mounts were fixed). | Medium |
| Q2 | `builder.go:345-354`, `build/uid.go:140-157`, `resolved.go:67` | The UID-update decision lives in three places with different rules; `resolved.UpdateRemoteUserUID` is read nowhere and defaults to the opposite of the spec. | Medium |
| Q3 | `cli/clean.go:47`, `doctor.go:180,359,397`, `status.go:53`, `lock.go:52`, `config.go:70`, `ssh_info.go:54` | Commands build Docker engines themselves, against CLAUDE.md, which is why `cli` is at 26.7% and every `run*` at 0%. `clean` calls the engine directly; `doctor` re-implements validation `ResolveWorkspace` enforces (mostly unreachable); `config` derives the workspace folder from the local config instead of `resolved.WorkspaceFolder`. | Medium |
| Q4 | `cli/doctor.go:420-424`, `selinux/` | SELinux support is claimed but not implemented: doctor says "will use :Z bind mount option", CLAUDE.md says the package is "for bind-mount labels", but nothing applies `:z`/`:Z`. On an enforcing Fedora host the workspace mount is unreadable while doctor reports green. | Medium |
| Q5 | `service/up.go:58,62,108` | `Up` uses `s.Identity()` next to the identity embedded in `resolved`, and resolves everything twice when there is work (the second time with `PullForMetadata`), so features resolve twice. | Low |
| Q6 | various | Vestigial fields and options: `Builder.logger`, `DevContainerService.builder`, `Provisioner.containerID`, `BuildOptions.UpdateLockfile`/`FrozenLockfile`, `SubstitutionContext.ContainerEnv` and `handleContainerEnv`, the no-op `DevContainerConfig.MarshalJSON`, impossible nil checks in `Provisioner`. `LockModeVerify` and `LockModeFrozen` differ only in error text. | Low |
| Q7 | various | Stale comments: "implements ContainerRuntime" (no such interface), `debug` in `resolve.go`, `resolved.go`'s note about untyped hooks and `BaseImage` for Dockerfile/compose, `installsAfter` in `features/manager.go`, `WaitForPostStartCommand` "(default behaviour)", getent claims, the ephemeral port note in `setupSSHAccess`, an orphaned line in `id.go`. | Low |
| Q8 | `CLAUDE.md` | Drift: `ImageBuilder` doesn't exist; `build` never runs docker; feature env isn't merged; state lookup uses `workspace.id` only; `installsAfter` listed in resolution; `StateUnknown` missing; plan structs and substitution variables incomplete; file tables miss `up.go`, `status.go`, `stop.go`, `lock_workspace.go`, `entrypoint.go`, `ssh_endpoint.go`, `lock.go`, `build/metadata.go`; the "commands never construct a Docker client" line is false (Q3). | Low |
| Q9 | `up.go:57`, `builder.go:56`, `doctor.go:56` | Oversized functions: split `Up`'s pre-step decision into `planUp`, `Builder.Build` into plan/users/runtime, and `runDoctor`'s two identical print loops into one helper. `renderStatus`, `ParseRunArgs`, `ParsePortSpec` and `extractTarReader` are flat and fine. | Low |
| Q10 | `metadata.go:122-163`, `builder.go:314`, `build/features.go`, `features/dockerfile.go` | Small duplications: `mergeStringMap`/`mergeAnyMap` vs generic `mergeMap`, `contains` vs `slices.Contains`, user/home defaulting done twice. | Low |
| Q11 | `report`, `container/docker.go` | `-v` adds five lines: there is no `Reporter.Debugf`, and docker command lines are never logged, so users can't see what failed or reproduce it. | Low |

## 4. Tests, CI and developer experience

| # | Where | Problem | Severity |
|---|---|---|---|
| T1 | `test/e2e/ssh_agent_e2e_test.go:80-106` | `github_ssh_connection` can't fail ("permission denied", what broken forwarding prints, passes) and depends on github.com. | High |
| T2 | `features/resolver.go`, `lock.go` | Feature fetching, registry auth, integrity checks and lockfile generate/verify are at 0% unit coverage; nothing uses `httptest`. This is the code that parses untrusted registry responses and enforces integrity. | High |
| T3 | `test/e2e/main_e2e_test.go` | e2e still writes to the real `~/.dcx`: host keys, known_hosts, workspace locks, compose overrides and the feature cache all derive from `$HOME`. This machine has 777 host keys and 122 lock files, almost all from test runs. Needs a `DCX_HOME` (or `HOME`) override in `TestMain`. | High |
| T4 | `ssh_agent_e2e_test.go` | SSH agent forwarding is never exercised in CI (no agent on runners, so the tests skip); one test duplicates another. | Medium |
| T5 | `container/secrets.go`, `service/status.go`, `build/features.go` | Cheap, risky paths at 0%: secret mounting (modes, owners, names), `Status` assembly over engine states, local feature staging (symlinks). | Medium |
| T6 | `dcx_config_e2e_test.go:294`, `new_commands_e2e_test.go` | A vacuous assertion (`NotContains "Shortcuts:"`, a label `status` never prints) and e2e tests that need no Docker (`run --list`, completion). | Medium |
| T7 | `test/conformance/` | Mostly JSON-unmarshal tests that duplicate `internal/devcontainer`; it never goes through `ResolveWorkspace` or runs spec fixtures. | Low |
| T8 | `test/e2e` | Not hermetic: 83 uses of `alpine:latest`, three node tags, an ubuntu image, live ghcr features, `apk add` from the network. | Low |
| T9 | `test/helpers`, `cli/commands_test.go` | `RunDCXInPTY` has no timeout; the planned `helpers.Workspace` (about 140 copied cleanup blocks) and golden `--help` tests (third audit's T9) never happened. | Low |
| T10 | — | Third-audit leftovers: the secrets e2e matrix (13 tests), `error_handling_e2e` as unit tests, compose caching tests matching log text. | Low |
| D1 | `.github/workflows/release.yml` | Releases aren't gated on CI: a `v*` tag runs lint and unit tests only (no e2e, deadcode or CI dependency). | Medium |
| D2 | `cli/upgrade.go:62,90,104`, `install.sh` | `dcx upgrade` and `install.sh` don't verify the published checksums; `http.Get` has no timeout; the version check is string equality, so a dev build "upgrades" to an older release. | Medium |
| D3 | `Makefile:72` | e2e runs under go test's default 10-minute timeout; a cold CI runner can cross it. | Medium |
| D4 | `.golangci.yml` | No gofmt/goimports formatter (mis-grouped imports exist), no `noctx`/`bodyclose`, e2e-tagged files never linted. | Low |
| D5 | CI | No macOS job, though darwin binaries are released. | Low |

## 5. CLI UX and docs

| # | Where | Problem | Severity |
|---|---|---|---|
| U1 | `cli/doctor.go:61-62` | `doctor --system --config-only` checks nothing and reports "All checks passed!". Same mutually-exclusive gap for `lock --verify/--frozen` and `clean --all/--dangling`. | Medium |
| U2 | `devcontainer/parser.go:106-107` | A relative `-c` resolves against the workspace, not the cwd (`-w ws -c ws/.devcontainer/devcontainer.json` looks for `ws/ws/…`). | Medium |
| U3 | `service/stop.go:75-79` | `restart --rebuild` is skipped by `shutdownAction: none` unless `--force`; the help says "without rebuilding" but a stale config recreates. | Medium |
| U4 | `container/docker.go`, `cli/doctor.go:365` | Permission-denied errors read "exit status 1: permission denied…", and doctor hints "Start Docker Desktop" when the daemon is running but the user lacks access. | Medium |
| U5 | `cli/*` | JSON gaps: errors in `--json` mode aren't JSON; key names differ (`workspace` vs `workspaceID`, `host` vs `sshAlias`, `buildMethod` vs `planType`); `ssh info --doctor --json` ignores `--json`; no `--json` on `doctor` or `lock`. | Low |
| U6 | `cli/root.go:36`, `ui/ui.go` | Colour leaks on flag and unknown-command errors (initUI runs after them); progress goes to stdout, mixing with data. | Low |
| U7 | `cli/run.go`, `ssh_info.go`, `root.go:108` | `run --help` shows "Usage:" twice; `run <TAB>` completes files instead of shortcuts; `ssh info` without a config blames the host key; `--quiet` is described differently in the flag help and README (warnings are in fact silenced). | Low |
| Doc1 | `README.md:87` | The "Using Go" install (`go install …@latest`) produces a binary without the agent (the `.gz` files aren't committed), so `dcx up` fails. | High |
| Doc2 | `README.md` | Undocumented: features (local/OCI/HTTP, entrypoints, ordering, the 24h tag re-check, lockfile on `up`), network modes, port behaviour, and the files dcx writes (`~/.dcx`, `DCX_SSH_DIR`). | Medium |
| Doc3 | `README.md` | No trust warning: `initializeCommand` and the `dcx.secrets` commands run on the host on `dcx up`, so opening an untrusted repo runs its commands. | Medium |
| Doc4 | `README.md:149,452` | It documents a `config --json` flag that doesn't exist (`config` always prints JSON, which its help doesn't say). | Medium |
| Doc5 | `README.md:304-317` | The lockfile section omits that `up`/`build` use it automatically and how CI should enforce it. | Low |

## Status

**Step 1 (common-path bugs): done**, each with a regression test.
- **F1:** the features Dockerfile switches to `USER root` and restores the base image's `USER` after the features. `Engine.ImageConfig` (user, entrypoint, cmd) replaces `ImageCommand`. Unit and e2e tested (a Dockerfile ending in `USER app` plus a feature that writes to `/usr/local`).
- **F2:** `forwardPorts` and `appPort` without a host address publish on `127.0.0.1`. Docker rejected the literal `localhost`, so `appPort` never worked. With compose `network_mode: service:<x>` every port (the user's and the agent's) is published on `<x>`; with host networking none are, and dcx says so. The e2e test now checks `docker port` for loopback and that `appPort` creates.
- **F3:** image layers' `customizations.dcx` are not merged, and secrets and build secrets are read from `resolved.Local`. Tested with an image label carrying `dcx.secrets`.
- **F7:** the SSH config step runs before the hooks.
- **F14:** postStart records a `poststart` marker with its time. A resume after the agent died runs postStart when the container's `StartedAt` is later; when that can't be told it runs anyway.
- **F10:** `IdentityFile`, `UserKnownHostsFile` and an absolute `Include` are quoted, with `%` doubled.
- **F11:** `--pull` and `--no-cache` reach the user's Dockerfile build, and `--no-cache` reaches the features layer.
- **F12:** `…@sha256:<hex>` parses into resource and digest version (`FeatureSource.IsDigest`); the canonical ID uses `@`.
- **F13:** a string feature value is the `version` option.
- **F18:** `down --volumes` removes a single container's named volumes. `ContainerDetails.Volumes` and `Engine.RemoveVolume` are new.
- **U1:** `doctor --config-only/--system`, `lock --verify/--frozen/--update` and `clean --all/--dangling` are mutually exclusive.
- **U2:** a relative `--config` is made absolute against the current directory.
- **U3:** `restart --rebuild` is checked before `shutdownAction: none`, and the restart help describes what it does.
- **Doc1:** the README says `go install` is unsupported and shows `make install`. The upgrade notes list this step's behaviour changes.

**Step 2 (staleness and identity): done**, with option (b) for F4.
- **F4:** the staleness key and a drift key are now separate.
  - `ComputeConfigHash`, the staleness key, covers devcontainer.json as written (parsed again without substitution), the Dockerfile and compose files, and local feature files.
  - `ComputeInputsHash`, the new `hash.inputs` label, covers the substituted config and feature digests (`FeaturesKey`).
  - `up` recreates only when the staleness key changes. When only the inputs changed, `up` and `status` report drift and keep the container.
  - `hash.version` is now 3, so existing containers are treated as current (with the one-time notice), not recreated.
  - Tested at three levels: hashing (a moved tag), resolution (`${localEnv:…}` in a mount) and `Up` (drift keeps the container).
- **F8:** `WorkspaceID(path, configPath)` keeps the path-only ID for `.devcontainer/devcontainer.json` and `.devcontainer.json`, and hashes path plus config otherwise. `${devcontainerId}` and identity use it. `up` warns about (and leaves alone) a container this config created under the folder's old ID.
- **F9:** a compose top-level `name:` gives `<name>-<id8>`. Existing environments keep their recorded project.
- **F5:** the UID layer ends with the image's own `USER` (`Engine.ImageConfig`).
- **F15:**
  - `Up`, `Down` (new, used by the CLI, and now also retiring the host key), `Stop` and `Restart` take the workspace lock.
  - `Restart` holds it across the stop and the up (internal `up`).
  - `DownWithIDs` stays unlocked for `Up`'s recreate.
- **F17:** the fallback client key is created with a unique temp file plus a link, so the first writer wins. The public key is always derived from the private one, and a missing or mismatched `.pub` is rewritten.

**Step 3 (image metadata done properly): done.**
- **S1:** `metadataImage` finds the image whose metadata applies without Docker.
  - Image plans use the image.
  - Dockerfile plans use the target stage's `FROM`, following stage names and substituting ARG defaults and build args.
  - Compose plans use the primary service's `image`, or the `FROM` of its build, across all compose files with `${VAR}` interpolation.
  - `up` pulls that image for its metadata on the first run, and the golden view pins `metadataImage` for every case.
  - Built images are labelled with the FROM image's metadata plus the local config. The features layer uses the metadata resolution read (no duplicate local entry).
- **F6:** image metadata entries are substituted like devcontainer.json. `id` and `entrypoint` are modelled and written for features. `resolved.Entrypoints` (image metadata, then features, deduplicated) is what runs at start.
- **S9:** a single-object `devcontainer.metadata` label is accepted.
- **S3:** image metadata's `hostRequirements` merge max-wise (the stronger GPU requirement wins). CPU and memory shortfalls warn, `storage` is no longer reported, and a missing GPU runtime still fails.
- **S2:** the `waitFor` machinery only timed a misleading "Container is ready" message, so it is removed. All hooks finish before `up` returns, which is documented.
- **Tests:** unit tests for FROM and compose base images, Dockerfile-base metadata, substitution and entrypoints, single-object labels, and host requirements. A new e2e test builds a local base image whose label sets `remoteUser`, a hook and an entrypoint, then builds `FROM` it.

## 6. Plan

Each step ships on its own and is a few hundred lines.

1. **Common-path bugs, each with a regression test.**
   - F1: features install as root (`USER root`, image user restored).
   - F2: ports. `appPort` binds `127.0.0.1`; `forwardPorts` is published on loopback, or forwarded through the SSH block's `LocalForward`; compose `service:` mode publishes on the owner.
   - F3: dcx customizations come from the local config only.
   - F7: write the SSH config before the hooks.
   - F10: quote paths in the SSH config.
   - F11: `--no-cache`/`--pull` reach Dockerfile builds.
   - F12, F13, F14, F18, U1, U2, U3, Doc1.
2. **Staleness and identity.**
   - F4: no silent recreate from volatile inputs. Hash `${localEnv:…}`-derived mount sources and unpinned feature digests out of the staleness key, or make a stale container that differs only in those inputs say so instead of recreating. This needs a decision; see below.
   - F8: config-aware workspace ID that stays stable for single-config users.
   - F9: a compose `name:` gets the workspace suffix.
   - F5: the UID layer keeps the image's user.
   - F15: down/stop/restart take the workspace lock.
   - F17.
3. **Image metadata done properly.**
   - S1: two-phase resolve for Dockerfile and compose bases.
   - F6: substitute image metadata; model `entrypoint` and `id` and chain image-metadata entrypoints.
   - S9, S3 (merge max-wise; cpu and memory shortfalls warn), S2 (run phases after `waitFor` in the background, or drop the message and document that dcx waits).
4. **Seams and cleanup.**
   - Q3: an engine factory in `context.go`, image cleanup behind the service, doctor reduced to "resolution succeeded" plus file checks. Then command-level tests.
   - Q1, Q2 with S6, Q4 (apply `:z` on enforcing SELinux, or stop claiming it), Q5, Q6, Q7, Q9, Q10, Q11 (`Reporter.Debugf`, docker argv at `-v`).
   - S5, S7, S8, S10–S14, F16, F19, F20, F21.
5. **Tests and CI.**
   - T3 first (`DCX_HOME`).
   - T1, T2 (an `httptest` OCI registry: token, manifest, blob, digest and integrity mismatches, traversal, pins vs moved tags), T4–T10.
   - D1, D3, D4, D5.
6. **UX and docs.** D2 (checksums, timeout, semver, `--force` to downgrade), U4–U7, Doc2–Doc5, the CLAUDE.md fixes (Q8), S4 (private registries).

**Decision needed before step 2 (F4):** today a config change recreates the container automatically, which is convenient but destroys container-local state. The options are (a) keep auto-recreate but remove volatile inputs from the key, (b) recreate automatically only for edits to devcontainer.json, Dockerfiles and compose files, and only report other drift, or (c) never recreate without `--recreate`. Option (a) is the smallest change; (b) is the most predictable.
