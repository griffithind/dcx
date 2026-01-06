# dcx - Devcontainer Executor

A CLI that parses, validates, and runs devcontainers without using the @devcontainers/cli. Designed for offline-safe operations with full support for docker compose devcontainers and Features.

## Features

- **Native Docker integration** - Uses Docker Engine API and docker compose CLI directly
- **Offline-safe operations** - `start`, `stop`, `exec`, and `shell` commands work without network access
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
dcx start                 # Start containers (offline-safe)
dcx down                  # Remove containers
```

## Commands

| Command | Offline-Safe | Description |
|---------|--------------|-------------|
| `dcx up` | No | Build/pull images and start environment |
| `dcx build` | No | Build images without starting |
| `dcx start` | Yes | Start existing containers |
| `dcx stop` | Yes | Stop running containers |
| `dcx exec` | Yes | Run command in container |
| `dcx shell` | Yes | Interactive shell |
| `dcx down` | Yes | Stop and remove containers |
| `dcx status` | Yes | Show environment state |
| `dcx doctor` | Partial | Check system requirements |
| `dcx upgrade` | No | Update dcx to latest version |

## Global Flags

```
-w, --workspace   Workspace directory (default: current directory)
-c, --config      Path to devcontainer.json
-v, --verbose     Enable verbose output
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

Use `--no-agent` on `exec`, `shell`, or `up` commands to disable SSH forwarding.

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
- [Architecture](docs/design/ARCHITECTURE.md)

## License

[MIT License](LICENSE)
