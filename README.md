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
