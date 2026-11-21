# devcontainers/cli compatibility review

dcx compared with the official devcontainers/cli v0.90.0 (commit `a190a3f`, 2026-10-30), to find where a `devcontainer.json` that works with the official CLI (and VS Code, which uses the same code) behaves differently in dcx, or where the two tools' artifacts don't interoperate.

## How this was produced

- **Source.** The upstream repository was shallow-cloned and read. Nothing from it was installed or run.
- **Four parallel comparisons,** each reading the upstream code and the dcx counterpart:
  - config resolution and substitution;
  - image metadata, lifecycle hooks, environment and users;
  - features;
  - the container runtime and compose.
- **Verification.** The high-impact findings were checked by hand in both codebases. One claim was wrong and is dropped: upstream does not resolve `${containerEnv:…}` inside lifecycle commands either, because hooks run from a map built without container substitution.
- **IDs.** IDs say where a finding came from: C (config), M (metadata and lifecycle), F (features), R (runtime). Duplicates found by two comparisons are merged under one ID. Upstream paths are relative to the upstream `src/`.

- **Recorded fixtures.** `test/conformance/testdata/official/` holds workspaces together with what the official CLI (the same version) made of them: `read-configuration` (install order, merged configuration) and `upgrade` (the lockfile), plus the image metadata, compose model and registry features it read. `TestOfficialCLI` resolves each workspace offline from those inputs and compares. `test/conformance/record-official.sh` re-records them by hand (Docker and network access needed; the CLI runs in a container). It is never run by the tests. Add a case when a compatibility fix needs one.

dcx's deliberate differences are listed at the end, not as findings: SSH sessions to an agent, `customizations.dcx` secrets, `waitFor` ignored, its recreate policy, and its own labels and image names.

## Status

**Step 1 (parsing and resolution): done.**
- **Parsing and decoding.** Configs and image labels are read into a JSONC tree (`tree.go`), and the raw and substituted configs are both decoded from it.
  - **C19:** a BOM is accepted.
  - **C20:** mis-cased property names are dropped with a warning.
  - **C16:** every string is substituted in one pass with the reference implementation's `${...}` pattern; `customizations`, `hostRequirements` and the rest are now included.
  - **M15:** image metadata decodes per property, so a bad value drops only that property, with a warning.
- **Config values.**
  - **C8:** `StringMap`/`NullableStringMap` accept numbers and booleans.
  - **C7:** `build.cacheFrom` is a string or an array, written back in the form read, so config hashes don't change.
  - **C6:** the legacy `dockerFile`/`context` win over a `build` object, as upstream.
  - **C10:** `image` + `build` is valid; the built image is also tagged as `image`, and a `build` without a dockerfile next to `image` is an image config. Compose with either is still rejected.
  - **C14:** `"workspaceMount": ""` means no mount, and an invalid `workspaceMount` fails validation.
- **Build paths.** **C4:** absolute paths are kept. **C5:** the default context is the Dockerfile's directory.
- **Substitution rules.**
  - **C13:** a variable set to an empty string gives the empty string, upstream's rule; defaults keep colons, which upstream truncates, and this is kept deliberately.
  - **C21:** the basename of `/` is empty.
- **C15/M8:** image metadata no longer supplies `workspaceFolder`, `workspaceMount`, `runArgs` or `features`.
- **F13:** `devcontainer-feature.json` is read as JSONC.

**Step 2 (mounts, ports and labels): done.**
- **Workspace layout** (`workspace.go`).
  - **C2/R4:** the nearest folder with a `.git` entry (directory, or a worktree's file) is mounted at `/workspaces/<folder>`, and the default `workspaceFolder` is the workspace's place in it. dcx looks for `.git` instead of running `git rev-parse --show-cdup`; the result is the same.
  - **C3/R3:** `workspaceFolder` no longer moves the mount.
  - A layout other than the plain bind of the workspace at the workspace folder is part of `hash.inputs`, so containers created with the old layout report drift instead of breaking.
- **Ports.**
  - **R1/M12:** `"localhost:N"` is port N; an entry whose host port is already published (earlier in `appPort`/`forwardPorts`, or by the compose file on the network owner) is skipped, silently when it is the same port and with a warning otherwise.
  - **R12:** `appPort` numbers bind `127.0.0.1`, strings are passed as written.
  - The port-conflict hint names the port and suggests `--recreate` only for the SSH port; it now also applies on create.
- **R8:** a volume the user's compose files declare is not re-declared in the override. Volumes only in `mounts` are still declared with their bare name, as `docker run --mount` uses them (an intentional difference: upstream prefixes them with the project).
- **M10:** the container's `devcontainer.metadata` label is the merged metadata (image, features, local config).
- **R5:** `runArgs` `--name` names the container.
- **C17** (done later, after step 5): see below.

**Step 3 (features): done.**
- **F1:** the lockfile is devcontainers/cli's: keys as written, integrity is the OCI manifest digest (features are fetched by it), `dependsOn` in declaration order, keys ordered as `localeCompare` and the same JSON form. A lockfile an older dcx wrote fails with a hint to run `dcx lock`.
- **F14:** `up` and `build` write the lockfile when it is an empty file. Writing it on every `up` (upstream's default) and `--frozen-lockfile` on `up` are not done: `dcx lock` and `dcx lock --frozen` cover them.
- **F2, F11, F12:** references follow `getRef`: lowercased, `devcontainers-contrib` → `devcontainers-extra`, `name:tag@sha256:…`, a registry must be a host (the GitHub-release form is rejected rather than fetched), http for `localhost`; legacy short ids are mapped (and `maven`/`gradle`/`jupyterlab` become options); tarballs must be https and named `devcontainer-feature-<id>.tgz`; local features must be inside `.devcontainer` (or the config's folder, for configs elsewhere), absolute paths are rejected.
- **F3/M1:** without `containerUser`, features install for the compose service's `user`, else the image's `USER`.
- **F4, F5:** `containerEnv` is set before the install step; options are a per-feature `devcontainer-features.env` (defaults plus every key the user set), no longer build ARGs.
- **F6–F9:** the round algorithm, with features identified by source and options, `installsAfter` a hard constraint among present features (renames followed through the referenced feature's `legacyIds`), and `overrideFeatureInstallOrder` a priority. An override entry that matches no feature warns (upstream only fails for one it cannot fetch). An `installsAfter` entry that is no feature reference is ignored with a warning (upstream fails).
- **F10:** a devcontainer.json mount wins over a feature's on the same target.
- **F16:** `false` installs a feature with its defaults; the label's feature `id` is the user's reference. (A `true` or `false` value is not turned into a `version` option, which upstream does for features with one.)
- **F15:** manifests are hashed and checked against the pinned digest instead of trusting `Docker-Content-Digest`; `DEVCONTAINERS_OCI_AUTH` and identity tokens (OAuth2 refresh) are supported.
- The features image tag has a version, so images built with the old Dockerfile are rebuilt on the next create.

**Step 4 (users, environment and lifecycle): done.**
- **M3:** `/etc/environment` gets the container's `Config.Env` (dcx's block, rewritten each up); `remoteEnv` is never written there.
- **M2/C11:** the UID update user is `remoteUser`/`containerUser`, else the `runArgs` `--user` or compose `user:`, else the image's `USER` (decided in `layerImages` once the image is known); root and numeric users are skipped. A compose config without features may now get a UID layer. The UID script (upstream's) chowned the user's home even when it is `/` (`nobody`); dcx now chowns only a home directory other than `/` and matches the passwd entry from the line start.
- **M4/M14:** the session shell, and the env probe's, is the container's `SHELL`, else the passwd shell, else `/bin/sh`. A `nologin` or `false` shell (system users such as `nobody`) is skipped; upstream would use it and fail every command.
- **M13:** without a configured user the agent runs sessions as `id -un` (the uid only when it has no name).
- **M6:** `initializeCommand` runs on every `up`, also when nothing else does.
- **M7:** parallel commands all finish before the phase fails.
- **M5:** container hooks run on a PTY when dcx's stdout is a terminal. Unlike upstream, not in CI (no terminal), so logs keep plain line endings and stderr stays separate.

**Step 5 (runtime details): done.**
- **R6/C12:** a GPU the host lacks never fails `up`: `--gpus` is dropped, a required GPU warns, an optional one is silent. `HostRequirementWarnings` only warns now.
- **R13:** `docker create` without `-t -i -w` (the `WorkingDir` spec field is gone). A `workspaceFolder` outside the mount is no longer created empty by `-w`; sessions start in `$HOME` there, as upstream's do.
- **R14:** without the buildx plugin, `docker build` with `DOCKER_BUILDKIT=1`; build contexts (the features layer) still need buildx and say so.
- **R15:** the keep-alive is upstream's `while sleep 1 & wait $!; do :; done`.
- **R16:** a stopped compose environment starts with `up -d --no-recreate [services]` instead of `start`. On create dcx has no existing container (a recreate removes it first), so the flag only applies to starts.
- **R10:** `compose build` builds only `runServices` and the primary (all services without `runServices`).
- **R11:** `docker compose config` is read with `--profile '*'`, and since review item 4 it is the only reading of compose files: resolution reads the model once (`ComposeConfigSource`) for the metadata image, the hashes' Dockerfiles and provisioning, so `.env`, `extends`, `include` and override files in other folders behave as in compose. Services in profiles are not started without `runServices` (upstream neither).
- **Ordering (review item 4):** `initializeCommand` runs before resolution, as upstream's `resolveWithLocalFolder` does, so it can generate compose files, Dockerfiles or features.
- **M9:** the `devcontainer.metadata` labels dcx writes keep the base entries verbatim, string mounts as written and devcontainer.json unsubstituted (`resolved.Raw`); feature ids were already the user's references (F16).
- **C17:** port ranges in docker `-p` syntax (`"3000-3005"`, `"8000-8005:3000-3005"`, `"IP::3000-3005"`, with a protocol) publish one binding per port (`ParsePortSpecs`), so collisions and dedupe work per port. A host range for a single container port (Docker picks one host port) is rejected, and a range is capped at 1024 ports.

Decisions made on 2026-11-14 (see "Decisions" below):
- `forwardPorts` stay published and are deduplicated;
- dcx keeps its own identity;
- the git root is mounted by default.

## 1. Interoperability (dcx and the official CLI/VS Code on the same project)

| ID | Where | Finding | Severity |
|----|-------|---------|----------|
| F1 | `features/lock.go:63`, `resolver.go:545` | Lockfile `integrity` for OCI features is the layer blob digest in dcx, but the **manifest** digest upstream (`lockfile.ts:28-33`, `containerFeaturesConfiguration.ts:1001`), which upstream also fetches by. A lockfile written by `devcontainer up` (written by default in v0.90) makes `dcx up` fail with "integrity mismatch", and the reverse fails too. Key case and `dependsOn` order differ too (dcx lowercases and sorts). | High |
| C1 | `devcontainer/id.go:45-76`, `substitute.go:343` | `${devcontainerId}` uses another algorithm. Upstream: sha256 of the sorted JSON `{"devcontainer.config_file": <abs config>, "devcontainer.local_folder": <abs folder>}`, as 52 base-32 characters (`variableSubstitution.ts:161-170`). Volumes named with it (docker-in-docker's `/var/lib/docker`, user `mounts`) are never shared with VS Code or the CLI. | High |
| R2 | `devcontainer/id.go:88-106`, `compose.go:57` | Compose project name. Upstream (`dockerCompose.ts:640-681`) tries `COMPOSE_PROJECT_NAME` (environment, then `.env`), then the compose `name:`, then `<folder>_devcontainer`. dcx always uses `<name>-<id8>` and passes `-p`, so named volumes (`myapp_devcontainer_pgdata` vs `myapp-ab12cd34_pgdata`) and `docker compose -p` scripts don't match. | High |
| R9, M10 | `container/provisioner.go:636-678` | Containers carry only `com.griffithind.dcx.*` labels. There are no `devcontainer.local_folder`/`config_file` labels, so VS Code and the CLI create a second container for the folder. With no features or UID layer, there is no container `devcontainer.metadata` label with the local entry, so "Attach to Running Container" misses `remoteUser`. | Medium |
| R8 | `container/compose_override.go:93-101` | Volumes from `mounts` are declared in the compose override with `name:`, so they are not project-prefixed as upstream's are. A same-named volume in the user's compose file silently switches to the unprefixed one. | Medium |
| M9 | `build/metadata.go:64-176` | The `devcontainer.metadata` label dcx writes on built images:<br>- holds the substituted config, so host paths and `${devcontainerId}` are baked in;<br>- re-serialises string mounts as objects and loses their options;<br>- uses short feature ids. | Low |

## 2. Configs that fail or differ in dcx

| ID | Where | Finding | Severity |
|----|-------|---------|----------|
| R1 | `container/provisioner.go:733-750` | `forwardPorts` are published with `docker -p` (also for compose and from image metadata); upstream publishes only `appPort` and leaves `forwardPorts` to the tool's forwarding. A compose `ports: ["3000:3000"]` plus `forwardPorts: [3000]` fails with "address already in use". Two workspaces forwarding 5432, or a host postgres, collide. A server bound to 127.0.0.1 in the container is unreachable through a publish. The port-conflict hint wrongly suggests `--recreate`. | High |
| F2 | `features/types.go` | No `devcontainers-contrib` → `devcontainers-extra` rewrite (`containerCollectionsOCI.ts:222-224`). Old configs that use `ghcr.io/devcontainers-contrib/features/*` get the old, unmaintained packages. | High if the old namespace is gone (to check), otherwise Low |
| C8 | `devcontainer/config.go` | Number or boolean values in `containerEnv`, `remoteEnv` or `build.args` (`"PORT": 3000`) make the whole config fail to load. Upstream passes them through. Verified. | Medium |
| C7, R7 | `devcontainer/config.go:127` | A string `build.cacheFrom` (allowed by the schema) fails to load. | Medium |
| C4 | `devcontainer/builder.go:173-178` | An absolute `build.context` or `dockerfile` (e.g. `${localWorkspaceFolder}`) is joined onto the config dir, giving a nonexistent path. | Medium |
| C5 | `devcontainer/builder.go:172` | With no `context`, the build context is the config dir; upstream uses the Dockerfile's directory (`utils.ts:501-507`). `"dockerfile": "../docker/dev.Dockerfile"` builds with the wrong context. | Medium |
| C6 | `devcontainer/parser.go:28-31` | Legacy top-level `dockerFile`/`context` together with a `build` object (older templates) is refused. | Medium |
| C3, R3 | `container/provisioner.go:487` | `workspaceFolder` without `workspaceMount` moves the bind target to `workspaceFolder`. Upstream still mounts at `/workspaces/<name>` and only changes the cwd. A Dockerfile that builds into `/app` with `"workspaceFolder": "/app"` has its built content hidden. | Medium |
| C2, R4 | `container/provisioner.go:487`, `substitute.go:505-517` | The git root is not mounted. Upstream's `--mount-workspace-git-root` is on by default: it mounts the repository root and sets the cwd to `/workspaces/<repo>/<subdir>`. In a monorepo with the config in a subfolder, dcx mounts only the subfolder, so there is no `.git` and siblings are missing. | Medium |
| R6, C12 | `devcontainer/hostreqs.go:53-60` | `hostRequirements.gpu: true` on a host without GPUs fails `up`. Upstream warns and starts without GPUs. | Medium |
| R5 | `devcontainer/runargs.go:84-86` | `runArgs --name` is dropped silently. Upstream honours it, and `--name myproj_devcontainer` is a common pattern. | Medium |
| C10 | `devcontainer/validate.go:47-77` | `image` + `build` is refused. Upstream builds and tags the image as `image`, and with a `build` that has no Dockerfile it is an image plan. | Low-Medium |
| C13 | `devcontainer/substitute.go:206,259` | `${localEnv:X:default}`:<br>- upstream applies the default only when the variable is unset; dcx also applies it when it is empty;<br>- upstream's default stops at the next colon; dcx keeps colons in it. | Low |
| C14 | `container/provisioner.go:484-488` | `"workspaceMount": ""` (no mount) still mounts the project. | Low |
| C16 | `devcontainer/substitute.go:366-441` | Only listed fields are substituted. `customizations`, `portsAttributes` and `hostRequirements` are not. Patterns apply one after another, so substituted values can expand again. | Low |
| C19, C20, C21 | `devcontainer/parser.go`, `substitute.go:290` | Parsing and substitution edge cases:<br>- C19: a UTF-8 BOM fails to parse in dcx (upstream's behaviour wasn't checked);<br>- C20: keys match case-insensitively;<br>- C21: `${containerWorkspaceFolderBasename}` of `/` is `/` in dcx and `""` upstream. | Low |
| C15, M8 | `devcontainer/metadata.go:93-128` | `workspaceFolder`, `workspaceMount`, `runArgs` and `features` are taken from image labels; upstream never takes them from metadata. | Low |
| M15 | `devcontainer/metadata.go:14-28` | One mistyped value in a third-party `devcontainer.metadata` label drops the whole label. Upstream uses entries loosely. | Low |
| R11 | `container/provisioner.go:961`, `baseimage.go:146-212` | Compose is read without `--profile '*'`, so a primary service in a profile is not found. `.env` interpolation, `extends` and `include` are ignored when finding the metadata image. | Low |
| R12, C17 | `devcontainer/ports.go:28-90` | String `appPort` entries are bound to loopback (`"8080:80"` becomes LAN-unreachable). Port ranges are rejected. | Low |

## 3. Features

| ID | Where | Finding | Severity |
|----|-------|---------|----------|
| F3, M1 | `features/dockerfile.go:27-35` | Without `remoteUser`/`containerUser`, features install for root. Upstream uses the image's `USER`, or the compose service's `user:` (`containerFeatures.ts:385-389`). Hidden whenever the image metadata sets `remoteUser` (all mcr images). | Medium |
| F4 | `features/dockerfile.go:121-122` | A feature's `containerEnv` is set after its install.sh runs; upstream sets it before (`containerFeaturesConfiguration.ts:336`). | Medium |
| F5 | `features/dockerfile.go:162-165`, `types.go:277` | Option values are `ARG`s and leak into every later feature: common-utils' `USERNAME=automatic` reaches docker-in-docker. Options a feature doesn't declare are dropped. Upstream writes a per-feature env file containing the defaults plus every key the user set. | Medium |
| F6 | `features/ordering.go:42-61` | `overrideFeatureInstallOrder` is absolute in dcx, overriding `dependsOn`. Upstream treats it as a priority within rounds that still honour dependencies, and an unknown entry is an error there. | Medium |
| F7, F8 | `features/ordering.go:150-226` | `installsAfter` is a soft score in dcx and can be violated; upstream treats it as a hard constraint among listed features. Tie-breaking differs (upstream: rounds sorted by registry/namespace/id), so valid orders differ too. | Medium |
| F9 | `features/manager.go:97-241` | Feature identity:<br>- features are deduplicated by bare name, so two `common-utils` from different registries collapse, and `./node` satisfies `…/features/node`;<br>- a `dependsOn` entry's own options are discarded. | Low |
| F10 | `devcontainer/builder.go:293-299` | A feature's mount wins over the user's mount on the same target. Upstream's order is image, features, devcontainer.json, so the user wins. | Medium |
| F11 | `features/types.go:233-235` | Legacy short ids (`"golang"`, `"node"`, `"docker-in-docker"`) are rejected. Upstream maps them to `ghcr.io/devcontainers/features/*`, with a warning. | Low |
| F12 | `features/types.go:212-249` | Reference parsing:<br>- references are case-sensitive;<br>- `name:tag@sha256:…` produces a broken URL;<br>- dcx assumes the ghcr.io registry for 2-segment ids; upstream treats those as the GitHub-release form;<br>- localhost registries are always reached over https;<br>- tarballs over http and absolute or out-of-tree local paths are allowed. | Low |
| F13 | `features/resolver.go:642` | A `devcontainer-feature.json` with comments or trailing commas fails; upstream parses it as JSONC. | Low |
| F14 | `devcontainer/resolve.go:74` | `up`/`build` only read the lockfile; upstream also writes it by default, and `--frozen-lockfile` fails on a mismatch. An empty lockfile is an error in dcx; upstream treats it as "create one". | Low |
| F15 | `registryauth`, `resolver.go:477` | No `DEVCONTAINERS_OCI_AUTH` or ACR `identitytoken`. The manifest body isn't checked against a pinned `@sha256` (the `Docker-Content-Digest` header is trusted). | Low |
| F16 | `features/manager.go:79`, `build/metadata.go:150` | A feature set to `false` is skipped; upstream still installs a v2 feature with its defaults. The label `id` is the short name, not the user's reference. | Low |

## 4. Lifecycle, environment and users

| ID | Where | Finding | Severity |
|----|-------|---------|----------|
| M3 | `service/devcontainer.go:232-252` | `remoteEnv` (tokens included) is written to world-readable `/etc/environment`. Upstream writes the container's `Config.Env` there and passes `remoteEnv` only to processes. Verified. | Medium |
| M2, C11 | `devcontainer/builder.go:196-205,395-403` | The UID update is skipped when the user comes only from the image `USER` or the compose `user:`. A numeric `remoteUser` is not skipped. Upstream uses `remoteUser`, then the runArgs or compose user, then the image `USER`, and skips root and numeric users. Bind-mounted files then have the wrong owner. | Medium |
| M4, M14 | `agent/cli.go:269-277`, `env/probe.go:35-39` | Sessions use the first of `/bin/bash`, `/bin/zsh`, `/bin/sh`, ignoring the user's login shell, while the env probe uses the passwd shell. The container's `SHELL` is ignored. | Medium |
| M13 | `agentctl/provision.go:50,113-119`, `agent/server.go:252-265` | Without `remoteUser`/`containerUser`, sessions run as a numeric uid: `USER=1000` and a guessed `HOME=/home/1000`. Upstream uses the image's `USER` by name. | Low |
| M6 | `service/up.go:129-133` | `initializeCommand` is skipped when the container is already running. Upstream runs it on every `up`. | Low |
| M7 | `lifecycle/hooks.go:154-167` | In the object (parallel) form, dcx cancels the sibling commands when one fails. Upstream lets every command finish, then fails. | Low |
| M5 | `lifecycle/hooks.go` (then `ssh/client/client.go`) | Hooks run without a PTY; upstream runs them with one. `docker run -it` in a hook fails, and output has no colours. | Low |
| R13 | `container/docker.go:461-472` | The container is created with `-t -i -w <workspaceFolder>`; upstream passes none of these. With `overrideCommand: false`, an image command like `bash` keeps running in dcx but exits upstream. | Low |
| R14, R15, R16 | `docker.go:921-960`, `spec.go:70-74`, `provisioner.go:199-209` | Smaller runtime differences:<br>- R14: dcx always runs `buildx build`; upstream falls back to plain `docker build`;<br>- R15: dcx keeps the container alive with `sleep infinity`; upstream uses a portable `sleep 1` loop;<br>- R16: no `--no-recreate` on compose create;<br>- R10: dcx builds every compose service; upstream builds only the primary and `runServices`. | Low |

## 5. Gaps (upstream features dcx doesn't have)

- **M16:** dotfiles (`--dotfiles-repository`, installed after postCreate, before postStart).
- **M17:** `--skip-post-create`, `--skip-post-attach`, `--default-user-env-probe`, `--update-remote-user-uid-default`.
- **Others:** `--mount-git-worktree-common-dir` (opt-in, C22); `--id-label`, `--override-config`, `--expect-existing-container`.

Following the "only what's necessary" principle, the plan does not add these unless a user asks for them.

## Intentional differences (kept)

- **Sessions:** SSH to the dcx agent instead of a VS Code server or `exec`. `postAttachCommand` runs per interactive SSH session.
- **Secrets:** they come from `customizations.dcx` commands and are mounted on a `/run/secrets` tmpfs.
- **`waitFor`:** ignored; `up` waits for every hook.
- **Lifecycle:** once-markers are written after a phase succeeds, so a failed phase is retried on the next `up`. Upstream marks before running.
- **Recreate policy:** an edit to devcontainer.json, a Dockerfile or a compose file recreates the container; other drift is only reported. Upstream never recreates on its own.
- **Naming and layers:** images are tagged `dcx/<id>:<layer>`, features are a separate layer, and dcx uses its own `com.griffithind.dcx.*` labels.
- **Extensions:**
  - `${userHome}` and `${pathSeparator}`;
  - a `build` without a dockerfile;
  - relative or `~` bind sources;
  - `.devcontainer/<sub>/devcontainer.json` discovery;
  - any `--config` file name;
  - warnings for `hostRequirements` cpus and memory.
- **Feature handling:** a 24-hour tag cache with an offline fallback, and `_REMOTE_USER_HOME` resolved per feature.

## Decisions

1. **`forwardPorts` (R1): keep publishing them** on `127.0.0.1`, which is a dcx extension (ports are always up, without an SSH session). Changes:
   - deduplicate (`appPort` and `forwardPorts`, `3000` and `"localhost:3000"`; also M12);
   - skip a port the compose service already publishes;
   - fix the port-conflict hint, which wrongly suggests `--recreate`.

   Two workspaces forwarding the same port still collide, and the error says so.
2. **Identity (C1, R2, R9): keep dcx's.** `${devcontainerId}`, labels and compose project names stay dcx's own, so dcx environments remain separate from VS Code and the official CLI. This is documented in the README as an intentional difference.

   Still fixed because they are bugs whatever the identity:
   - R8: a user compose volume re-declared with `name:`;
   - M10: the container's `devcontainer.metadata` label, which attach tools read;
   - F1: lockfile interop, since the lockfile is a shared file in the repository.
3. **Git root (C2): mount it by default,** as upstream does. The repository root is mounted at `/workspaces/<repo>` and the default `workspaceFolder` is the subfolder inside it. Nothing changes when the workspace is the repository root.

## Plan

Each step ships on its own, with tests.

1. **Parsing and resolution** (no decisions needed):
   - parsing: C8 scalar values, C7 string `cacheFrom`, C6 legacy plus `build`, C10 `image` + `build`, C19 BOM, C20 exact keys, F13 JSONC feature files;
   - resolution: C4 absolute paths, C5 default context, C13 default semantics, C14 empty `workspaceMount`, C16 substitute every string field once, C21;
   - metadata: C15/M8 the metadata property set, M15 tolerant labels.
2. **Mounts, ports and labels:** C2/R4 the git-root mount, C3/R3 the mount target, R1 dedupe and skip compose-published ports (plus M12 and the conflict hint), R12, R8, M10, R5 (`--name`), and the README note on identity.
3. **Features:**
   - F1 lockfile interop (manifest digest, keys as written), F14 (read the empty-lockfile marker);
   - F2 the contrib redirect, F11 legacy ids, F12 reference parsing;
   - F3/M1 users from the image or compose `user`;
   - F4 env before install, F5 per-feature env file;
   - F6–F9 the ordering algorithm in upstream's round form, F10 user mounts win, F16;
   - F15 manifest digest check.
4. **Users, environment and lifecycle:** M3 (`/etc/environment` gets `Config.Env`, not `remoteEnv`), M2/C11 the UID user, M4/M14 the login shell, M13 named users, M6 `initializeCommand` on every up, M7 let parallel commands finish, M5 a PTY for hooks.
5. **Runtime details:** R6/C12 GPU warning, R13 no `-t -i -w`, R14 a plain `docker build` fallback, R15 the portable keep-alive, R16 `--no-recreate`, R10 build only the needed services, R11 compose profiles and `.env`, M9 label fidelity.
