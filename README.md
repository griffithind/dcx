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

## Installation

### Quick Install (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/griffithind/dcx/main/install.sh | sh
```

Installs to `~/.local/bin/dcx`.

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
- `--hosts` - Allow SSH from more than loopback (`--hosts=10.0.0.0/24`, `--hosts=any`, `--hosts=loopback`). It is recorded on the container and kept by `up`, `restart` and `--recreate`; changing it needs `--recreate`

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
| `dcx config` | Show resolved configuration |

### Utilities

| Command | Description |
|---------|-------------|
| `dcx build` | Build images without starting |
| `dcx lock` | Generate or verify lockfile for reproducible builds |
| `dcx clean` | Remove unused dcx images (`--current`, `--all`, `--dangling`, `--dry-run`) |
| `dcx doctor` | Check system requirements |
| `dcx upgrade` | Update dcx to latest version |
| `dcx ssh info` | SSH alias, host key and connection details (`--json`, `--doctor`) |

`dcx status --detailed --json` shows what the old hidden `dcx debug` did:
the resolved plan and features, why a container is stale, and its labels.
JSON output (`status`, `list`, `config`, `ssh info`) uses stable camelCase
keys.

### Global Flags

```
-w, --workspace   Workspace directory (default: current directory)
-c, --config      Path to devcontainer.json
-v, --verbose     Enable verbose output
-q, --quiet       No progress output (data you asked for, errors and warnings still print)
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
  the alias, this one is reached as `<container>.dcx` (`dcx ssh info`
  shows it).
- **Container / compose project:** `<sanitized name>-<first 8 characters
  of the workspace id>`, so two checkouts with the same `name` never
  collide. A top-level `name:` in your compose files sets the compose
  project instead.

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
dcx lock              # Generate devcontainer-lock.json
dcx lock --verify     # Verify lockfile matches current resolution
dcx lock --frozen     # CI mode: fail if lockfile missing or mismatched
```

The lockfile pins:
- Exact semantic versions
- OCI manifest digests
- SHA256 integrity hashes

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

Hooks run in the foreground and `dcx up` waits for them, so start
long-running processes in the background (as above) or with your compose
file. A hook can be a string (run with `sh -c`), an array (run directly)
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

## Upgrading

Changes since 0.7 that affect existing setups:

- `dcx shell` and `dcx exec` are gone. Use `dcx ssh` and `dcx ssh -- <cmd>`
  (or plain `ssh <alias>`). `dcx ssh` needs the OpenSSH client.
- `dcx ssh` connects instead of printing the command; `dcx ssh info` shows
  the alias and connection details.
- dcx's SSH host blocks moved to `~/.ssh/dcx/config`, included from the top
  of `~/.ssh/config`. Existing blocks move there on the next `dcx up`.
- Shortcuts run through `sh -c`, so shell syntax in `command`/`prefix`
  works; extra arguments are passed unchanged as `"$@"`.
- `dcx plan` is gone; `dcx status` shows the state and whether the config
  changed. The hidden `dcx debug` is gone too: use `dcx status --detailed --json`.
- `--quiet` silences progress only; status, lists and other output you
  asked for still print. Colour is off when output is not a terminal.
- The agent runs as root and switches to the session user per session.
- `userEnvProbe` defaults to `loginInteractiveShell` (the spec default) and
  uses the user's login shell, so nvm/asdf PATH entries reach hooks and
  sessions. Set `"userEnvProbe": "none"` to turn it off.
- JSON keys of `config --json` and `ssh info --json` are camelCase.
- A config that sets more than one of `image`, `build` and
  `dockerComposeFile` is rejected instead of silently picking one.
- `forwardPorts` and `appPort` entries without a host address are
  published on `127.0.0.1` only (`forwardPorts` used to bind every
  interface, and `appPort` failed to create). Write `"0.0.0.0:3000:3000"`
  in `appPort` to expose a port on the network. With compose
  `network_mode: service:<x>` the ports are published on `<x>`; with host
  networking they are not published (they are already on the host).
- Features install as root, whatever the base image's `USER`; the image's
  user is restored afterwards. A feature given as a string
  (`"…/node:1": "18"`) sets its `version` option.
- A relative `--config` is relative to the current directory, not the
  workspace.
- `dcx restart --rebuild` is no longer skipped by `shutdownAction: "none"`.
- `dcx down --volumes` also removes a single container's named volumes.
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
