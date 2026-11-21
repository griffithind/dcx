# Changelog

## Unreleased (since 0.7.0)

dcx now follows the official devcontainers/cli wherever a `devcontainer.json`
means the same thing to both ([docs/compatibility.md](docs/compatibility.md)).
Existing containers keep working; `dcx up --recreate` picks up every change.

### Upgrading

- **Upgrade dcx** by running the install command again (`dcx upgrade` is gone).
- **After upgrading**, the first `dcx up` may report drift or say the container
  was created by an older dcx: run `dcx up --recreate` when convenient.
- **OpenSSH client required**: `dcx ssh` and container lifecycle hooks use it.
- **`docker compose` plugin required** to resolve compose configs, also for
  `dcx status` and `dcx config` (no running daemon needed).
- **Lockfiles written by an older dcx** fail with a hint: run `dcx lock`.
- **Old files** are cleaned up as you go: host keys move into
  `~/.dcx/workspaces/<id>/` when next used, and `dcx clean` removes what is
  left in `~/.dcx/hostkeys`, `locks` and `compose`. Old feature cache entries
  (`~/.cache/dcx/features/<16 hex digits>`) can be deleted.

### Commands

- Removed: `dcx shell`, `dcx exec` (use `dcx ssh`, `dcx ssh -- <cmd>` or
  `ssh <alias>`), `dcx ssh info` (`dcx status`, `dcx doctor`), `dcx plan`,
  `dcx debug` (`dcx status --detailed --json`), `dcx upgrade`, and `dcx run`
  with `customizations.dcx.shortcuts` (ignored if present).
- `dcx ssh` connects (it used to print the command).
- `dcx restart --rebuild` is no longer skipped by `shutdownAction: "none"`;
  `dcx down --volumes` also removes a single container's named volumes.
- `dcx build` uses build secrets, `--pull` and `--no-cache` as `up` does,
  including compose services.
- A relative `--config` is relative to the current directory.

### Output

- Data goes to stdout, progress and warnings to stderr; `--quiet` silences
  progress; colour is off when not on a terminal.
- JSON keys are camelCase; `list --json` calls the plan `planType`.
- Tables draw `│` columns with a rule under the header.

### Configuration

- Read like the official CLI: numbers and booleans in env maps and
  `build.args`, a string `build.cacheFrom`, legacy `dockerFile`/`context`
  next to `build`, a UTF-8 BOM; case-sensitive property names; every string
  substituted once (`customizations` and `hostRequirements` too).
- `dockerComposeFile` with `image` or `build` is rejected; `image` with
  `build` builds and tags the image.
- Without `build.context` the context is the Dockerfile's directory;
  absolute paths are used as they are.
- `"workspaceMount": ""` means no mount; `${localEnv:VAR:default}` uses the
  default only when `VAR` is unset.
- Image metadata applies to Dockerfile and compose configs too (from the
  image they build `FROM`), is variable-substituted, no longer supplies
  `workspaceFolder`, `workspaceMount`, `runArgs` or `features`, and a bad
  property drops only that property.
- `initializeCommand` runs on every `up`, before dcx reads anything else, so
  it can generate compose files, Dockerfiles or features.
- A config outside the default locations is its own environment.
- Changed inputs that are not edits (a `${localEnv:…}` value, a moved feature
  tag) are reported as drift instead of recreating the container.
- `hostRequirements` CPU/memory and a missing GPU warn instead of failing.

### Workspace, ports and compose

- The git repository containing the workspace is mounted; `workspaceFolder`
  only sets the working directory.
- `forwardPorts` without a host address and `appPort` numbers publish on
  `127.0.0.1`; `appPort` strings are passed as written; ranges work;
  duplicates and ports the compose file publishes are published once.
- Compose is read through `docker compose config` (all profiles), so `.env`,
  `extends`, `include` and override files in other folders behave as in
  compose. Services that build through `extends`/`include` may be recreated
  once. Only `runServices` and the primary service are built; a stopped
  environment starts with `up -d --no-recreate`.
- `runArgs` `--name` names the container; a compose file's own volume wins
  over a same-named `mounts` volume.

### Features and lockfile

- `devcontainer-lock.json` uses the official format (keys as written,
  integrity is the manifest digest); `up`/`build` write it when it is an
  empty file.
- Install order follows the official rounds: `installsAfter` is honoured,
  `overrideFeatureInstallOrder` is a priority, a dependency with other
  options installs again.
- Features install as root; options reach only their own feature;
  `containerEnv` is set before install; `_CONTAINER_USER` falls back to the
  compose `user` or the image's `USER`.
- Legacy short ids are mapped; references without a registry, absolute or
  out-of-tree local paths and plain-http tarballs are rejected; `false`
  installs with defaults; a user mount wins over a feature mount.
- Fetched features are stored by digest, so workspaces pinning different
  versions can build at once.

### Users, environment and hooks

- `/etc/environment` holds the container's environment, never `remoteEnv`.
- The UID update applies to the image's or compose service's user when there
  is no `remoteUser`; numeric users are left alone.
- Sessions without a configured user run as the image's user by name, with
  its login shell. `userEnvProbe` defaults to `loginInteractiveShell`.
- Hooks run over OpenSSH, on a pseudo-terminal when dcx runs in one; a
  failing parallel command no longer stops the others.

### Runtime

- Containers are created without `-t -i -w`; the keep-alive is a `sleep 1`
  loop; without buildx, images build with `docker build` (features still
  need buildx).
- Containers carry the merged `devcontainer.metadata` label, so editors that
  attach see `remoteUser` and the rest.
- The agent runs as root and switches to the session user.
- dcx builds with Go 1.27 and has fewer dependencies (no pterm, no
  self-update library, no in-process SSH client).
