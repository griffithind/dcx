# Compatibility with devcontainers/cli

dcx aims to read a `devcontainer.json` the way the official
[devcontainers/cli](https://github.com/devcontainers/cli) does (and VS Code,
which shares its code), and to share its files: features, image metadata and
`devcontainer-lock.json`. The reference is v0.90.0 (commit `a190a3f`).

**How it is checked.** `test/conformance` runs the spec's merge and default
rules through `ResolveWorkspace`. `test/conformance/testdata/official/` holds
workspaces with what the official CLI made of them (`read-configuration`:
install order and merged configuration; `upgrade`: the lockfile) and the
inputs it read (image metadata, compose model, registry features).
`TestOfficialCLI` resolves each one offline and compares.
`test/conformance/record-official.sh` re-records them by hand (Docker and
network needed; the CLI runs in a container). Add a case when a
compatibility change needs one.

## Intentional differences

- **Sessions**: SSH to the dcx agent instead of a VS Code server or `exec`;
  `postAttachCommand` runs per interactive session; hooks run over SSH,
  without a pseudo-terminal when dcx has no terminal (CI), so logs stay
  plain.
- **Identity**: dcx's own `${devcontainerId}`, labels
  (`com.griffithind.dcx.*`) and compose project names (`<name>-<id8>`), so
  dcx and VS Code environments of one folder stay separate (C1, R2, R9).
  Images are tagged `dcx/<id>:<layer>`, with features in their own layer.
- **Ports**: `forwardPorts` are published on `127.0.0.1` (upstream leaves
  them to the editor's forwarding), deduplicated, and skipped when the
  compose file publishes them (R1).
- **Secrets**: `customizations.dcx` commands run on the host, mounted on a
  `/run/secrets` tmpfs; no `--secrets-file`.
- **Lifecycle**: `waitFor` is ignored (`up` waits for every hook);
  once-markers are written after a phase succeeds, so a failed phase runs
  again.
- **Recreate policy**: editing devcontainer.json, a Dockerfile, a compose
  file or a local feature recreates the container; other drift is reported.
  Upstream never recreates on its own.
- **Lockfile**: written by `dcx lock`, and by `up`/`build` only when it is an
  empty file; upstream writes it on every `up` (F14).
- **Smaller choices**: `${localEnv:X:a:b}` keeps colons in the default
  (C13); volumes only in `mounts` keep their bare name in compose (R8); an
  unknown `overrideFeatureInstallOrder` entry or non-reference `installsAfter`
  entry warns instead of failing (F6–F9); `true`/`false` is not turned into a
  `version` option (F16); a `nologin` or `false` login shell is skipped
  (M4); a range of host ports for one container port is rejected (C17).
- **Extensions**: `${userHome}`, `${pathSeparator}`, a `build` without a
  Dockerfile, relative and `~` bind sources,
  `.devcontainer/<sub>/devcontainer.json`, any `--config` name, warnings for
  `hostRequirements` CPUs and memory, a 24-hour feature tag cache with an
  offline fallback, `_REMOTE_USER_HOME` per feature.

## Not implemented

Added only when someone needs them: dotfiles (`--dotfiles-repository`, M16);
`--skip-post-create`, `--skip-post-attach`, `--default-user-env-probe`,
`--update-remote-user-uid-default` (M17); `--mount-git-worktree-common-dir`
(C22), `--id-label`, `--override-config`, `--expect-existing-container`.

## Finding index

The 2026-11 review compared both codebases; every finding below is fixed and
behaves as upstream unless listed above. Code comments cite these IDs.

**Config (C)**
- C2/R4: the git root containing the workspace is mounted; the default
  `workspaceFolder` is the workspace's place in it.
- C3/R3: `workspaceFolder` sets the working directory, not the mount.
- C4, C5: absolute build paths are kept; the default context is the
  Dockerfile's directory.
- C6: legacy `dockerFile`/`context` win over a `build` object.
- C7/R7: `build.cacheFrom` may be a string.
- C8: numbers and booleans are accepted in env maps and `build.args`.
- C10: `image` + `build` builds and tags `image`; compose with either is
  rejected.
- C11/M2: the UID update targets `remoteUser`/`containerUser`, else the
  runArgs or compose user, else the image's `USER`; root and numeric users
  are skipped.
- C12/R6: a missing GPU warns (silently when optional).
- C13: a variable set to an empty string gives the empty string.
- C14: `"workspaceMount": ""` means no mount.
- C15/M8: image metadata never supplies `workspaceFolder`,
  `workspaceMount`, `runArgs` or `features`.
- C16: every string is substituted once.
- C17: port ranges in `docker -p` syntax.
- C19–C21: a BOM is accepted; property names are case-sensitive (others
  warn); the basename of `/` is empty.

**Metadata and lifecycle (M)**
- M1/F3: features install for `containerUser`, else the compose `user`, else
  the image's `USER`.
- M3: `/etc/environment` holds `Config.Env`, never `remoteEnv`.
- M4/M14: the session shell is the container's `SHELL`, else the login shell.
- M5: hooks run on a pseudo-terminal when dcx runs in one.
- M6: `initializeCommand` runs on every `up`, before resolution.
- M7: parallel commands all finish before the phase fails.
- M9: written `devcontainer.metadata` labels keep base entries verbatim,
  string mounts as written and devcontainer.json unsubstituted.
- M10: containers carry the merged `devcontainer.metadata` label.
- M12: `"localhost:N"` is port N.
- M13: without a configured user, sessions run as the image's user by name.
- M15: a bad property in image metadata drops only that property.

**Features (F)**
- F1: lockfile format (keys as written, integrity is the manifest digest,
  `dependsOn` in order, `localeCompare` key order).
- F2: `devcontainers-contrib` is read as `devcontainers-extra`.
- F4, F5: `containerEnv` is set before install; options are a per-feature
  env file.
- F6–F9: install order in upstream's rounds; features are identified by
  source and options; `installsAfter` is a hard constraint;
  `overrideFeatureInstallOrder` is a priority.
- F10: a user mount wins over a feature's on the same target.
- F11, F12: legacy short ids are mapped; references follow upstream's
  parsing (lowercase, `tag@digest`, a registry host, https tarballs, local
  features inside `.devcontainer`).
- F13: `devcontainer-feature.json` is JSONC.
- F15: manifests are checked against pinned digests;
  `DEVCONTAINERS_OCI_AUTH` and identity tokens work.
- F16: `false` installs with defaults; labels record the user's reference.

**Runtime (R)**
- R5: `runArgs` `--name` names the container.
- R8: a volume the compose file declares is not re-declared.
- R10: compose builds only `runServices` and the primary service.
- R11: compose is read through `docker compose config` with every profile,
  once, for metadata, hashing and provisioning.
- R12: `appPort` strings are passed to Docker as written.
- R13: containers are created without `-t -i -w`.
- R14: without buildx, images build with `docker build`.
- R15: the keep-alive is a `sleep 1` loop.
- R16: a stopped compose environment starts with `up -d --no-recreate`.
