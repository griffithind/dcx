# Quick Start Guide

## Installation

### Quick Install (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/griffithind/dcx/main/install.sh | sh
```

This installs dcx to `~/.local/bin/dcx`. Make sure this directory is in your PATH.

### Using Go Install

```bash
go install github.com/griffithind/dcx/cmd/dcx@latest
```

### From Source

```bash
git clone https://github.com/griffithind/dcx.git
cd dcx
make build
./bin/dcx --version
```

### Upgrading

```bash
dcx upgrade
```

## Prerequisites

- Docker Engine running
- docker compose CLI plugin installed
- (Optional) SSH agent running for SSH forwarding

Verify with:
```bash
dcx doctor
```

## Your First Devcontainer

### 1. Create a devcontainer.json

Create `.devcontainer/devcontainer.json` in your project:

```json
{
  "name": "My Dev Environment",
  "dockerComposeFile": "docker-compose.yml",
  "service": "app",
  "workspaceFolder": "/workspace"
}
```

### 2. Create docker-compose.yml

Create `.devcontainer/docker-compose.yml`:

```yaml
version: '3.8'
services:
  app:
    image: node:18
    volumes:
      - ..:/workspace:cached
    command: sleep infinity
```

### 3. Start the Environment

```bash
dcx up
```

This will:
- Parse your devcontainer.json
- Generate a compose override with dcx labels
- Start the containers via docker compose
- Set up SSH agent forwarding (if available)

### 4. Verify Status

```bash
dcx status
```

Output:
```
Workspace:  /home/user/myproject
Workspace ID: abcd1234efgh
State:      RUNNING

Primary Container:
  ID:       abc123def456
  Name:     dcx_abcd1234efgh-app-1
  Status:   Up 2 minutes
```

### 5. Run Commands

```bash
# Run a single command
dcx exec -- npm install

# Open an interactive shell
dcx shell
```

### 6. Stop and Restart

```bash
# Stop the environment (offline-safe)
dcx stop

# Start again - if containers exist and config unchanged, this is offline-safe
dcx up
```

### 7. Remove the Environment

```bash
dcx down
```

## Single-Container Setup

For simpler projects, you can use an image directly:

### Image-Based

Create `.devcontainer/devcontainer.json`:

```json
{
  "name": "Simple Node",
  "image": "node:18",
  "workspaceFolder": "/workspace",
  "postCreateCommand": "npm install"
}
```

Then run:
```bash
dcx up
```

### Dockerfile-Based

Create `.devcontainer/devcontainer.json`:

```json
{
  "name": "Custom Build",
  "build": {
    "dockerfile": "Dockerfile"
  },
  "workspaceFolder": "/workspace"
}
```

Create `.devcontainer/Dockerfile`:

```dockerfile
FROM node:18
RUN npm install -g typescript
```

Then run:
```bash
dcx build   # Build the image
dcx up      # Start the container
```

## Common Workflows

### Development Session

```bash
# Start your day
dcx up

# Work...
dcx exec -- npm run dev
dcx shell

# End your day
dcx stop
```

### Config Changes

When you modify devcontainer.json:

```bash
# dcx detects the change and recreates
dcx up
```

### Clean Slate

```bash
# Remove everything and start fresh
dcx down --volumes
dcx up
```

## Next Steps

- Read the [Command Reference](COMMANDS.md)
- Learn about [Configuration Options](CONFIGURATION.md)
- Troubleshoot with the [Troubleshooting Guide](TROUBLESHOOTING.md)
