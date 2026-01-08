# dcx - Devcontainer Executor

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

> **Warning:** This project is new and under active development. APIs and features may change without notice.

A lightweight, single-binary CLI for running devcontainers. Works offline, tracks state via Docker labels, and includes built-in SSH support—agent forwarding for Git operations and an SSH server for remote development with any editor.

## Features

- **Native Docker integration** - Uses Docker Engine API and docker compose CLI directly
- **Offline-safe operations** - `stop`, `exec`, `shell`, and more commands work without network access
- **Labels as database** - Container state tracked via Docker labels, no local state files required
- **SSH agent forwarding** - Automatic forwarding of SSH agent to containers via TCP proxy
- **SELinux support** - Automatic detection and :Z relabeling on enforcing systems
- **Compose support** - Full support for docker compose-based devcontainers
- **Self-updating** - Built-in `upgrade` command to update to latest version

## Installation

### Quick Install (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/griffithind/dcx/main/install.sh | sh
```

This installs dcx to `~/.local/bin/dcx`.

### Using Go Install

```bash
go install github.com/griffithind/dcx/cmd/dcx@latest
```

### From Source

```bash
git clone https://github.com/griffithind/dcx.git
cd dcx
make build
```

## Quick Start

```bash
# In a directory with a devcontainer.json
dcx up                    # Build and start the environment
dcx status                # Check current state
dcx exec -- npm install   # Run a command
dcx shell                 # Open an interactive shell
dcx stop                  # Stop containers (offline-safe)
dcx down                  # Remove containers
```

## Commands

| Command | Offline-Safe | Description |
|---------|--------------|-------------|
| `dcx up` | Partial | Build/pull images and start environment |
| `dcx build` | No | Build images without starting |
| `dcx stop` | Yes | Stop running containers |
| `dcx restart` | No | Stop and start containers |
| `dcx exec` | Yes | Run command in container |
| `dcx shell` | Yes | Interactive shell |
| `dcx run` | Yes | Execute shortcuts from dcx.json |
| `dcx down` | Yes | Stop and remove containers |
| `dcx status` | Yes | Show environment state |
| `dcx list` | Yes | List managed environments |
| `dcx logs` | Yes | View container logs |
| `dcx config` | Yes | Show resolved configuration |
| `dcx clean` | Yes | Remove orphaned dcx images |
| `dcx ssh` | Yes | SSH access to container |
| `dcx doctor` | Partial | Check system requirements |
| `dcx upgrade` | No | Update dcx to latest version |

## Global Flags

```
-w, --workspace   Workspace directory (default: current directory)
-c, --config      Path to devcontainer.json
-v, --verbose     Enable verbose output
-q, --quiet       Minimal output (errors only)
    --json        Output as JSON
    --no-color    Disable colored output
    --version     Show version
```

## Environment States

dcx tracks environment state using Docker labels:

- **ABSENT** - No managed containers exist
- **CREATED** - Containers exist but are stopped
- **RUNNING** - Primary container is running
- **STALE** - Configuration has changed since last build
- **BROKEN** - Inconsistent state, requires recreation

## Configuration Support

dcx supports the following devcontainer.json configurations:

### Compose-based
```json
{
  "name": "My App",
  "dockerComposeFile": "docker-compose.yml",
  "service": "app",
  "workspaceFolder": "/workspace"
}
```

### Image-based
```json
{
  "name": "My App",
  "image": "node:18",
  "workspaceFolder": "/workspace"
}
```

### Dockerfile-based
```json
{
  "name": "My App",
  "build": {
    "dockerfile": "Dockerfile"
  },
  "workspaceFolder": "/workspace"
}
```

## Label Schema

Containers are tagged with labels under the `io.github.dcx.*` namespace:

| Label | Description |
|-------|-------------|
| `io.github.dcx.managed` | "true" for dcx-managed containers |
| `io.github.dcx.env_key` | Stable identifier from workspace path |
| `io.github.dcx.config_hash` | Hash of configuration for staleness detection |
| `io.github.dcx.plan` | "compose" or "single" |
| `io.github.dcx.primary` | "true" for the main container |

## SSH Agent Forwarding

dcx automatically forwards your SSH agent to containers using a TCP-based proxy:

1. Host runs a TCP listener that connects to your local SSH agent
2. dcx binary is copied into the container
3. Container-side proxy creates a Unix socket and forwards to host via TCP
4. `SSH_AUTH_SOCK` is set to the container socket path

This approach works across all platforms (Docker Desktop, native Linux, Colima, Podman) without socket mounting issues.

Use `--no-agent` on `exec`, `shell`, `run`, or `up` commands to disable SSH forwarding.

## SELinux Support

On Linux systems with SELinux in enforcing mode, dcx automatically:

- Detects the SELinux mode
- Applies `:Z` relabeling to bind mounts
- Ensures proper container access to mounted directories

## Upgrading

```bash
# Check current version
dcx --version

# Upgrade to latest
dcx upgrade
```

## Development

```bash
# Build
make build

# Run tests
make test

# Build release binaries (with embedded Linux binaries for macOS)
make build-release

# Run with verbose output
./bin/dcx -v up
```

## Requirements

- Go 1.24+ (for building from source)
- Docker Engine
- docker compose CLI plugin

## Documentation

- [Quick Start Guide](docs/user/QUICKSTART.md)
- [Command Reference](docs/user/COMMANDS.md)
- [Configuration Guide](docs/user/CONFIGURATION.md)
- [Design Overview](docs/design/DESIGN.md)

## License

[MIT License](LICENSE) - Copyright (c) 2026 Griffith Industries Inc
