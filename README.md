# dcx

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go 1.24+](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://go.dev/)

A single-binary CLI for running [devcontainers](https://containers.dev/) with built-in SSH support. No Node.js required. Works with any editor.

## Highlights

- **Single binary** - Download and run, no runtime dependencies
- **Built-in SSH server** - Connect with VSCode, Zed, Cursor, Neovim, or any SSH client
- **SSH agent forwarding** - Git operations inside containers use your local keys
- **Native secrets** - Fetch secrets from 1Password and other CLIs on-demand
- **Smart rebuilds** - Only rebuilds when configuration actually changes
- **Reproducible builds** - Lock feature versions with `dcx lock`
- **Docker Compose** - Full support for compose-based devcontainers
- **Command shortcuts** - Define project-specific aliases in devcontainer.json

## Quick Start

```bash
# Install dcx
curl -fsSL https://raw.githubusercontent.com/griffithind/dcx/main/install.sh | sh

# Navigate to a project with devcontainer.json
cd myproject

# Start the devcontainer
dcx up

# Connect via SSH (auto-configured)
ssh myproject.dcx

# Or, from the workspace directory
dcx ssh                    # interactive shell
dcx ssh -- npm install     # run a command
```

## Editor Integration

dcx includes a built-in SSH server that works with any editor supporting remote development.

### Setup

```bash
dcx up
```

This starts your devcontainer and automatically configures SSH access. Your project name becomes the SSH host:

```bash
ssh myproject.dcx
```

### VSCode

1. Install the [Remote - SSH](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-ssh) extension
2. `Cmd/Ctrl+Shift+P` → "Remote-SSH: Connect to Host"
3. Select your project name

### Zed

1. `Cmd+Shift+P` → "projects: Open Remote"
2. Connect via SSH to your project name

### Cursor

Works the same as VSCode with Remote SSH.

### Other Editors

Any editor or tool that supports SSH will work. The SSH host is `<project-name>.dcx`.

## Trust

`dcx up` runs commands from the repository **on your host**, not in the
container: `initializeCommand`, and the `customizations.dcx.secrets` and
`buildSecrets` commands (which typically read from your password manager).
Opening a repository with dcx is like running its build script: only run
`dcx up` in repositories you trust, and read those fields in an unfamiliar
`devcontainer.json` first (`dcx config --raw` prints it).

## Installation

### Quick Install (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/griffithind/dcx/main/install.sh | sh
```

Installs to `~/.local/bin/dcx`, after checking the download against the
release's `checksums.txt`.

`go install github.com/griffithind/dcx/cmd/dcx@latest` is not a supported
install method: dcx embeds the agent binaries it deploys into containers,
which `make build` generates, so a `go install` build cannot start a
devcontainer ("agent binary not embedded"). Use the install script, a
release binary, or build from source.

### From Source

```bash
git clone https://github.com/griffithind/dcx.git
cd dcx
make build     # bin/dcx, with the agent binaries embedded
make install   # copies it to $(go env GOPATH)/bin
```

## Commands

### Lifecycle

| Command | Description |
|---------|-------------|
| `dcx up` | Start the devcontainer, building if necessary |
| `dcx down` | Stop and remove containers |
| `dcx stop` | Stop containers without removing |
| `dcx restart` | Restart containers without rebuilding |

**`dcx up` flags:**
- `--rebuild` - Force rebuild images
- `--recreate` - Force recreate containers
- `--pull` - Re-pull the base image and re-fetch features (for moving tags like `:latest`); applies when the container is created, so add `--recreate` for an existing one
- `--hosts` - Allow SSH from more than loopback (`--hosts=10.0.0.0/24`, `--hosts=any`, `--hosts=loopback`). It is recorded on the container and kept by `up`, `restart` and `--recreate`; changing it needs `--recreate`. SSH keys are always required; the address list is an extra gate, and where Docker proxies published ports (Docker Desktop, rootless Docker) every client appears to come from the Docker gateway, so the list cannot tell them apart

**`dcx down` flags:** `--volumes` (also remove named volumes), `--remove-orphans`

**`dcx stop` / `dcx restart` flags:** `--force` (ignore `shutdownAction: "none"`); `restart --rebuild`

### Execution

| Command | Description |
|---------|-------------|
| `dcx ssh` | Open an interactive shell (via OpenSSH, same session as `ssh <alias>`) |
| `dcx ssh -- <cmd>` | Run a command in the container |
| `dcx run <shortcut>` | Run a command shortcut defined in devcontainer.json |

### Information

| Command | Description |
|---------|-------------|
| `dcx status` | Show devcontainer status (`--detailed`, `--json`) |
| `dcx list` | List all managed devcontainers (`--all`, `--json`) |
| `dcx logs` | View container logs |
| `dcx config` | Print devcontainer.json after variable substitution, as JSON (`--raw`: as written; `--validate`) |

### Utilities

| Command | Description |
|---------|-------------|
| `dcx build` | Build images without starting |
| `dcx lock` | Generate or verify the lockfile for reproducible builds (`--verify`, `--frozen`, `--json`) |
| `dcx clean` | Remove unused dcx images (`--current`, `--all`, `--dangling`, `--dry-run`) |
| `dcx doctor` | Check Docker, the configuration and, when it is running, the workspace's SSH agent (`--json`) |
| `dcx upgrade` | Update dcx to the latest release, verified against its checksums (`--force` to downgrade or replace a development build) |

`dcx status` shows the SSH alias and endpoint; `--detailed` adds the host
key fingerprint, the allowed source addresses, the resolved plan and
features, why a container is stale, and its labels.

Output you asked for (status, lists, JSON) goes to stdout; progress and
warnings go to stderr, so `dcx config > config.json` and `dcx list --json |
jq` see only data. JSON output (`status`, `list`, `config`, `doctor`,
`lock`) uses stable camelCase keys, and with `--json` a failure is printed
as `{"error": "..."}` on stdout too.

### Global Flags

```
-w, --workspace   Workspace directory (default: current directory)
-c, --config      Path to devcontainer.json
-v, --verbose     Enable verbose output
-q, --quiet       Only data and errors: no progress, no warnings
    --no-color    Disable colored output (also off with NO_COLOR or when not on a terminal)
```

## Configuration

### DCX Customizations

DCX-specific settings are defined in your `devcontainer.json` under `customizations.dcx`:

```json
{
  "name": "myproject",
  "image": "node:20",
  "customizations": {
    "dcx": {
      "shortcuts": {
        "test": "npm test",
        "dev": { "prefix": "npm run", "passArgs": true },
        "lint": { "command": "npm run lint", "description": "Run linter" }
      }
    }
  }
}
```

### Project Naming

- **Name:** `name` in devcontainer.json, or the workspace directory name.
  It is what `dcx status` and `dcx list` show.
- **SSH alias:** `<sanitized name>.dcx` (`My App` → `my_app.dcx`); with no
  `name` it is `<workspace id>.dcx`. When another workspace already uses
  the alias, this one is reached as `<container>.dcx` (`dcx status`
  shows it).
- **Container / compose project:** `<sanitized name>-<first 8 characters
  of the workspace id>`, so two checkouts with the same `name` never
  collide. A top-level `name:` in your compose files replaces the name
  part (`<name>-<id>`), so two checkouts never share a compose project.
  `runArgs` `--name` names the container instead (dcx finds its containers
  by label, not by name).
- **Separate from VS Code and the official CLI:** dcx uses its own
  workspace id (`${devcontainerId}`), labels and compose project names, so
  `dcx up` and VS Code's "Reopen in Container" on the same folder make two
  environments, and volumes named with `${devcontainerId}` are not shared.
  The files they share (devcontainer.json, features, `devcontainer-lock.json`)
  are read the same way.

### Shortcuts

Shortcuts can be defined as:

- **Simple string**: `"test": "npm test"`
- **Object with prefix**: `"dev": { "prefix": "npm run", "passArgs": true }` - runs `npm run <args>`
- **Object with command**: `"lint": { "command": "npm run lint" }`

Run shortcuts with:

```bash
dcx run test
dcx run dev start    # runs: npm run start
dcx run --list       # show available shortcuts
```

### Secrets

DCX takes a different approach to secrets than the reference devcontainer CLI:

- **Reference CLI**: Requires a pre-populated JSON file via `--secrets-file`, injects secrets as environment variables to lifecycle hooks
- **DCX**: Executes commands on your host to fetch secrets on-demand, mounts them as files at `/run/secrets/<name>`

#### Runtime Secrets

Secrets are mounted as files at `/run/secrets/<name>` after container starts:

```json
{
  "name": "myapp",
  "image": "node:20",
  "customizations": {
    "dcx": {
      "secrets": {
        "DATABASE_URL": "op read op://Development/database/connection-string",
        "API_KEY": "op read op://Development/api/key"
      }
    }
  }
}
```

Access in container:
```bash
cat /run/secrets/DATABASE_URL
```

**Security:**
- Stored in tmpfs (in-memory, never written to disk)
- Owned by `remoteUser` (accessible to non-root users)
- File permissions: 0400 (read-only by owner)

#### Using Secrets as Environment Variables

To use secrets as environment variables, add them to your shell profile in a lifecycle hook:

```json
{
  "name": "myapp",
  "image": "node:20",
  "postCreateCommand": "echo '[ -f /run/secrets/DATABASE_URL ] && export DATABASE_URL=$(cat /run/secrets/DATABASE_URL)' >> ~/.bashrc",
  "customizations": {
    "dcx": {
      "secrets": {
        "DATABASE_URL": "op read op://Development/database/url"
      }
    }
  }
}
```

Or load all secrets into the shell profile:

```json
{
  "postCreateCommand": "echo 'for f in /run/secrets/*; do export $(basename $f)=$(cat $f); done' >> ~/.bashrc"
}
```

#### Build Secrets

Available during `docker build` via BuildKit's `--mount=type=secret`. Works with both Dockerfile and Docker Compose builds:

```json
{
  "name": "myapp",
  "build": {
    "dockerfile": "Dockerfile"
  },
  "customizations": {
    "dcx": {
      "buildSecrets": {
        "NPM_TOKEN": "op read op://Development/npm/token"
      }
    }
  }
}
```

Use in Dockerfile:
```dockerfile
RUN --mount=type=secret,id=NPM_TOKEN \
    NPM_TOKEN=$(cat /run/secrets/NPM_TOKEN) npm install
```

Build secrets are never baked into image layers - they're only available during the build step that mounts them.

### Lockfiles

Lock feature versions for reproducible builds:

```bash
dcx lock              # Generate or update devcontainer-lock.json
dcx lock --verify     # Check that the lockfile matches what dcx resolves
dcx lock --frozen     # Same, and fail when there is no lockfile (CI)
```

`devcontainer-lock.json` sits next to `devcontainer.json`. For each OCI or
HTTP feature (local features are never locked) it pins the version, the
OCI manifest digest and the SHA-256 of the feature tarball.

- `dcx up` and `dcx build` use the lockfile automatically when it exists:
  a feature is fetched by its locked digest (a tag that moved to a new
  release is ignored) and must match its integrity hash, or `up` fails.
  They never write the lockfile; run `dcx lock` after changing features.
- Commit the lockfile, and run `dcx lock --frozen` in CI so a missing or
  stale lockfile fails the build.

## Build Options

DCX supports three build strategies:

### Image

Use a pre-built image:

```json
{
  "image": "mcr.microsoft.com/devcontainers/go:1"
}
```

### Dockerfile

Build from a Dockerfile:

```json
{
  "build": {
    "dockerfile": "Dockerfile",
    "context": "..",
    "args": {
      "GO_VERSION": "1.22"
    },
    "target": "development"
  }
}
```

### Docker Compose

Use Docker Compose for multi-container setups:

```json
{
  "name": "myapp",
  "dockerComposeFile": "docker-compose.yml",
  "service": "app",
  "workspaceFolder": "/workspace"
}
```

The `service` field specifies which container to use as the primary devcontainer. Other services in the compose file start automatically.

### Features

[Features](https://containers.dev/implementors/features/) are installed
into a derived image (see the pipeline below). A feature can come from:

- an OCI registry: `"ghcr.io/devcontainers/features/node:1": {}` (a tag,
  or `@sha256:…`);
- a local directory next to the config: `"./features/my-feature": {}`;
- an HTTP(S) tarball: `"https://example.com/feature.tgz": {}`.

Private registries use the credentials `docker login` stored: the docker
config (`$DOCKER_CONFIG/config.json`, default `~/.docker/config.json`),
including `credHelpers` and `credsStore`, and `$GITHUB_TOKEN` for ghcr.io.

Fetched features are cached in `~/.cache/dcx/features` (`$XDG_CACHE_HOME`).
A tag (`:1`, `:latest`) is checked against the registry again after 24
hours, and the cached copy is used when the registry cannot be reached;
`dcx up --pull --recreate` re-fetches now. Digests and lockfile pins are
never re-fetched.

Features install in dependency order: `dependsOn` (installed first,
fetched if not listed), then `installsAfter`, then
`overrideFeatureInstallOrder`. They install as root whatever the image's
`USER`. Feature `entrypoint`s run when the container starts, before its
command (after those recorded in the base image's metadata). A feature's
`containerEnv` is set in the image; its mounts, capabilities, `privileged`
and `init` apply to the container.

### Build Pipeline

When features are configured, DCX builds a derived image:

```
Base Image → Features → UID Sync → Final Image
```

Each layer is cached. Use `dcx up --rebuild` to force a fresh build.

## Runtime

### Lifecycle Hooks

Commands run at specific points in the container lifecycle:

```json
{
  "onCreateCommand": "npm install",
  "postCreateCommand": "npm run setup",
  "postStartCommand": "nohup npm run dev > /tmp/dev.log 2>&1 &"
}
```

Hooks run in the foreground and `dcx up` waits for all of them (`waitFor`,
which tells editors when they may connect, has no effect on a
command-line `up`), so start long-running processes in the background (as
above) or with your compose file. A hook can be a string (run with `sh -c`), an array (run directly)
or an object of named commands (run in parallel). Hooks from the base
image's metadata run first, then those of features (in install order),
then devcontainer.json's.

| Hook | When | Runs |
|------|------|------|
| `initializeCommand` | Before container creation (on host) | Once |
| `onCreateCommand` | After container created | Once |
| `updateContentCommand` | When content is updated | Once |
| `postCreateCommand` | After onCreate | Once |
| `postStartCommand` | After container starts | Every start |
| `postAttachCommand` | When client connects | Every attach |

### Workspace Mount

As in the official CLI, the git repository containing the workspace is
mounted at `/workspaces/<repository folder>`, and the default
`workspaceFolder` is the workspace's place inside it: a config in
`monorepo/services/api` gets `/workspaces/monorepo/services/api`, with
`.git` and the sibling folders available. A workspace that is the
repository root, or is not in a repository, is mounted itself at
`/workspaces/<folder>`. `workspaceFolder` changes only the working
directory; set `workspaceMount` to mount somewhere else (`""` for no
mount). Compose configs mount the workspace in the compose file.

### Ports and Networking

- `forwardPorts` entries without a host address, and `appPort` numbers,
  are published on `127.0.0.1` only. `appPort` strings are passed to
  Docker as written (`"8080:80"` is reachable from the network,
  `"0.0.0.0:3000:3000"` too).
- A port listed more than once (in `appPort` and `forwardPorts`, or as
  `3000` and `"localhost:3000"`) is published once, and a port your compose
  file already publishes is not published again. When a host port is in
  use, `dcx up` says which one.
- With compose `network_mode: service:<x>` the ports (and the SSH agent's
  port) are published on `<x>`. With host networking nothing is published:
  the ports, and the agent, are already on the host.
- The SSH agent listens on a loopback port chosen when the container is
  created (`dcx status` shows it); `--hosts` widens who may connect.

### Environment Variables

```json
{
  "containerEnv": {
    "NODE_ENV": "development"
  },
  "remoteEnv": {
    "EDITOR": "vim"
  }
}
```

- `containerEnv` - Set at container creation
- `remoteEnv` - Set in shell sessions

### User Configuration

```json
{
  "remoteUser": "node",
  "updateRemoteUserUID": true
}
```

When `updateRemoteUserUID` is true, DCX updates the container user's UID/GID to match your host user, ensuring file permissions work correctly with mounted volumes.

## Files dcx Writes

| Path | Contents |
|------|----------|
| `~/.dcx/` (`$DCX_HOME`) | Per-workspace SSH host keys, `known_hosts`, a fallback client key, workspace locks, compose override files |
| `~/.ssh/dcx/config` | The SSH host blocks for each workspace, included by a line at the top of `~/.ssh/config` (`$DCX_SSH_DIR` replaces `~/.ssh` for both) |
| `~/.cache/dcx/features` | Fetched features |
| `devcontainer-lock.json` | Written only by `dcx lock` |

`dcx down` removes the workspace's host key and SSH block. Docker state
(containers, images tagged `dcx/<workspace>`, volumes) is labelled
`com.griffithind.dcx.*`; `dcx clean` removes unused dcx images.

## Upgrading

Changes since 0.7 that affect existing setups:

- `dcx shell` and `dcx exec` are gone. Use `dcx ssh` and `dcx ssh -- <cmd>`
  (or plain `ssh <alias>`). `dcx ssh` needs the OpenSSH client.
- `dcx ssh` connects instead of printing the command.
- `dcx ssh info` is gone: `dcx status` shows the alias and endpoint
  (`--detailed` adds the host key), and `dcx doctor` probes the agent.
- dcx's SSH host blocks moved to `~/.ssh/dcx/config`, included from the top
  of `~/.ssh/config`. Existing blocks move there on the next `dcx up`.
- Shortcuts run through `sh -c`, so shell syntax in `command`/`prefix`
  works; extra arguments are passed unchanged as `"$@"`.
- `dcx plan` is gone; `dcx status` shows the state and whether the config
  changed. The hidden `dcx debug` is gone too: use `dcx status --detailed --json`.
- Progress and warnings go to stderr; stdout carries only the output you
  asked for. `--quiet` silences progress and warnings. Colour is off when
  output is not a terminal.
- The agent runs as root and switches to the session user per session.
- `userEnvProbe` defaults to `loginInteractiveShell` (the spec default) and
  uses the user's login shell, so nvm/asdf PATH entries reach hooks and
  sessions. Set `"userEnvProbe": "none"` to turn it off.
- JSON keys are camelCase (`dcx config` always prints JSON). `dcx list
  --json` calls the plan `planType` (it was `buildMethod`), as `status
  --json` does.
- `dcx upgrade` verifies the release's checksums and no longer replaces a
  development or newer build unless given `--force`.
- `dockerComposeFile` together with `image` or `build` is rejected instead
  of silently picking one. `image` with `build` is accepted, as in the
  official CLI: the image is built and also tagged as `image` (a `build`
  without a `dockerfile` next to `image` is ignored).
- Read like the official devcontainers/cli: numbers and booleans in
  `containerEnv`, `remoteEnv` and `build.args`, a string `build.cacheFrom`,
  legacy `dockerFile`/`context` next to a `build` object, and a UTF-8 BOM
  are accepted. Property names are case-sensitive (`"Image"` is ignored,
  with a warning). Every string is variable-substituted once, including
  `customizations` and `hostRequirements`.
- Without `build.context`, the build context is the Dockerfile's directory
  (it was the config's directory); absolute `dockerfile`/`context` paths
  are used as they are.
- `"workspaceMount": ""` means no workspace mount, and an invalid
  `workspaceMount` is an error.
- `${localEnv:VAR:default}` uses the default only when `VAR` is unset (a
  variable set to an empty string gives the empty string).
- Image metadata no longer supplies `workspaceFolder`, `workspaceMount`,
  `runArgs` or `features` (the official CLI ignores them there), and one
  bad property in an image's `devcontainer.metadata` drops only that
  property, with a warning.
- `forwardPorts` entries without a host address and `appPort` numbers are
  published on `127.0.0.1` only (`forwardPorts` used to bind every
  interface, and `appPort` failed to create); `appPort` strings are passed
  to Docker as written. A port listed twice, or already published by the
  compose file, is published once. With compose `network_mode:
  service:<x>` the ports are published on `<x>`; with host networking they
  are not published (they are already on the host).
- The git repository containing the workspace is mounted, and
  `workspaceFolder` no longer moves the mount (it stays at
  `/workspaces/<folder>`; set `workspaceMount` to move it). Containers
  created with the old layout report drift; `dcx up --recreate` applies it.
- `runArgs` `--name` names the container (it was ignored).
- Containers carry the merged `devcontainer.metadata` label, so editors
  that attach to a running container see `remoteUser` and the rest.
- A volume in `mounts` that your compose file also declares is the compose
  file's volume (dcx used to re-declare it under its bare name).
- Features install as root, whatever the base image's `USER`; the image's
  user is restored afterwards. A feature given as a string
  (`"…/node:1": "18"`) sets its `version` option.
- A relative `--config` is relative to the current directory, not the
  workspace.
- `dcx restart --rebuild` is no longer skipped by `shutdownAction: "none"`.
- `dcx down --volumes` also removes a single container's named volumes.
- The base image's `devcontainer.metadata` now also applies to Dockerfile
  and compose configs (read from the image they build `FROM`), so e.g.
  `remoteUser` from an mcr devcontainers image is honoured. Image metadata
  is variable-substituted, and the entrypoints recorded in it (features of
  a prebuilt image, such as docker-in-docker) run.
- `hostRequirements` CPU and memory minimums warn instead of failing `up`;
  image metadata's requirements merge by taking the larger value.
- Changed inputs that are not edits (a `${localEnv:...}` value that differs
  between logins, a feature tag that moved to a new release) no longer
  recreate the container: `dcx up` and `dcx status` report the drift, and
  `dcx up --recreate` applies it. Editing devcontainer.json, a Dockerfile,
  a compose file or a local feature still recreates. The first `dcx up`
  after upgrading says the container was created by an older dcx.
- A config outside the default locations (`.devcontainer/<name>/devcontainer.json`,
  `--config`) is its own environment, so several configs of one folder
  coexist. A top-level compose `name:` gets the workspace suffix
  (existing environments keep their project).
- Containers from older versions keep working; run `dcx up --recreate` to
  pick up every change.

## Shell Completions

```bash
# Bash
dcx completion bash > /etc/bash_completion.d/dcx

# Zsh
dcx completion zsh > "${fpath[1]}/_dcx"

# Fish
dcx completion fish > ~/.config/fish/completions/dcx.fish
```

## Development

```bash
# Build
make build

# Run tests
make test

# Build release binaries
make release

# Run with verbose output
./bin/dcx -v up
```

### Requirements

- Go 1.24+
- Docker Engine
- docker compose CLI plugin

## Built With

Developed using [Claude Code](https://claude.ai/code) by Anthropic.

## License

[MIT License](LICENSE) - Copyright (c) 2026 Griffith Industries Inc
