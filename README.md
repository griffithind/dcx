# dcx

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go 1.24+](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://go.dev/)

A lightweight, single-binary CLI for running [devcontainers](https://containers.dev/) with built-in SSH support for seamless editor integration.

## Quick Start

```bash
# Install
curl -fsSL https://raw.githubusercontent.com/griffithind/dcx/main/install.sh | sh

# Navigate to a project with a devcontainer.json
cd myproject

# Start the devcontainer
dcx up

# Run commands
dcx exec -- npm install
dcx shell
```

## Editor Integration

dcx includes a built-in SSH server that lets you connect with any editor that supports remote development.

### Setup

```bash
dcx up
```

This starts your devcontainer and automatically configures SSH access. Your project name becomes the SSH host, so you can connect with:

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

## Features

- **Single binary** - No runtime dependencies, just download and run
- **Built-in SSH server** - Automatically configured, connect with VSCode, Zed, Cursor, or any SSH client
- **SSH agent forwarding** - Your local SSH keys are automatically available for Git operations inside containers
- **Docker Compose support** - Full support for compose-based devcontainers
- **Devcontainer features** - Install tools and runtimes from the features ecosystem
- **Command shortcuts** - Define project-specific commands in devcontainer.json
- **Self-updating** - Run `dcx upgrade` to update to the latest version

## Installation

### Quick Install (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/griffithind/dcx/main/install.sh | sh
```

This installs dcx to `~/.local/bin/dcx`.

### Using Go

```bash
go install github.com/griffithind/dcx/cmd/dcx@latest
```

### From Source

```bash
git clone https://github.com/griffithind/dcx.git
cd dcx
make build
```

## Commands

### Lifecycle

| Command | Description |
|---------|-------------|
| `dcx up` | Build and start the devcontainer |
| `dcx down` | Stop and remove containers |
| `dcx stop` | Stop running containers |
| `dcx restart` | Restart containers |

### Execution

| Command | Description |
|---------|-------------|
| `dcx exec -- <cmd>` | Run a command in the container |
| `dcx shell` | Open an interactive shell |
| `dcx run <shortcut>` | Run a command shortcut defined in devcontainer.json |

### Information

| Command | Description |
|---------|-------------|
| `dcx status` | Show devcontainer status |
| `dcx list` | List all managed devcontainers |
| `dcx logs` | View container logs |
| `dcx config` | Show resolved configuration |

### Utilities

| Command | Description |
|---------|-------------|
| `dcx build` | Build images without starting |
| `dcx clean` | Remove orphaned dcx images |
| `dcx doctor` | Check system requirements |
| `dcx upgrade` | Update dcx to latest version |
| `dcx ssh` | SSH connection info |

### Global Flags

```
-w, --workspace   Workspace directory (default: current directory)
-c, --config      Path to devcontainer.json
-v, --verbose     Enable verbose output
-q, --quiet       Minimal output (errors only)
    --json        Output as JSON
    --no-color    Disable colored output
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

Project name resolution order:
1. **Compose projects**: `name` field in compose.yaml (preferred)
2. `name` field in devcontainer.json
3. Workspace directory name (fallback)

The resolved name is used for:
- Container and Docker Compose project naming
- SSH host (`myproject.dcx`)
- Display in `dcx status`

#### DCX Options

| Field | Description |
|-------|-------------|
| `customizations.dcx.shortcuts` | Command aliases for `dcx run` |

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
make build-release

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
