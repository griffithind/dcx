# Configuration Reference

This document describes the devcontainer.json fields supported by dcx.

## Plan Types

dcx supports two configuration plans:

### Compose Plan
Uses Docker Compose to orchestrate multiple services.

```json
{
    "name": "My Project",
    "dockerComposeFile": "docker-compose.yml",
    "service": "app",
    "workspaceFolder": "/workspace"
}
```

### Single-Container Plan
Uses a single container with either a pre-built image or a Dockerfile.

**Image-based:**
```json
{
    "name": "My Project",
    "image": "mcr.microsoft.com/devcontainers/base:ubuntu",
    "workspaceFolder": "/workspace"
}
```

**Dockerfile-based:**
```json
{
    "name": "My Project",
    "build": {
        "dockerfile": "Dockerfile",
        "context": "."
    },
    "workspaceFolder": "/workspace"
}
```

## Supported Fields

### Basic Configuration

| Field | Type | Description | Supported |
|-------|------|-------------|-----------|
| `name` | string | Display name | ✓ |
| `image` | string | Container image | ✓ |
| `dockerComposeFile` | string/array | Compose file(s) | ✓ |
| `service` | string | Primary service name | ✓ |
| `runServices` | array | Services to run | ✓ |

### Build Configuration

| Field | Type | Description | Supported |
|-------|------|-------------|-----------|
| `build.dockerfile` | string | Dockerfile path | ✓ |
| `build.context` | string | Build context | ✓ |
| `build.args` | object | Build arguments | ✓ |
| `build.target` | string | Build target stage | ✓ |
| `build.cacheFrom` | array | Cache sources | ✓ |

### Workspace Configuration

| Field | Type | Description | Supported |
|-------|------|-------------|-----------|
| `workspaceFolder` | string | Container workspace path | ✓ |
| `workspaceMount` | string | Custom workspace mount | ✓ |

### User Configuration

| Field | Type | Description | Supported |
|-------|------|-------------|-----------|
| `remoteUser` | string | User for remote operations | ✓ |
| `containerUser` | string | User for container | ✓ |
| `updateRemoteUserUID` | boolean | Update user UID to match host (Linux only, default: true) | ✓ |

### Environment Variables

| Field | Type | Description | Supported |
|-------|------|-------------|-----------|
| `containerEnv` | object | Container environment | ✓ |
| `remoteEnv` | object | Remote environment | ✓ |

### Mounts and Volumes

| Field | Type | Description | Supported |
|-------|------|-------------|-----------|
| `mounts` | array | Additional mounts | ✓ |

Mount string format:
```
source=/path/on/host,target=/path/in/container,type=bind
```

### Runtime Options

| Field | Type | Description | Supported |
|-------|------|-------------|-----------|
| `runArgs` | array | Docker run arguments | ✓ |
| `privileged` | boolean | Run privileged | ✓ |
| `init` | boolean | Run init process | ✓ |
| `capAdd` | array | Add capabilities | ✓ |
| `securityOpt` | array | Security options | ✓ |
| `overrideCommand` | boolean | Override container command | ✓ |
| `shutdownAction` | string | Action on shutdown | ✗ |

### Lifecycle Commands

| Field | Type | Description | Supported |
|-------|------|-------------|-----------|
| `initializeCommand` | string/array/object | Run on host before build | ✓ |
| `onCreateCommand` | string/array/object | Run after container creation | ✓ |
| `updateContentCommand` | string/array/object | Run after onCreateCommand | ✓ |
| `postCreateCommand` | string/array/object | Run after updateContentCommand | ✓ |
| `postStartCommand` | string/array/object | Run every container start | ✓ |
| `postAttachCommand` | string/array/object | Run every attach | ✓ |

### Port Forwarding

| Field | Type | Description | Supported |
|-------|------|-------------|-----------|
| `forwardPorts` | array | Ports to forward | ✓ |
| `portsAttributes` | object | Port attributes | Partial |

Port forwarding example:
```json
{
    "forwardPorts": [8080, 3000, "9000:9000"],
    "portsAttributes": {
        "8080": {
            "label": "Web Server",
            "protocol": "http"
        }
    }
}
```

Ports can be specified as:
- Integer: `8080` - Maps container port 8080 to host port 8080
- String: `"8080:80"` - Maps container port 80 to host port 8080
- String with IP: `"127.0.0.1:8080:80"` - Binds to specific host IP

### Features

| Field | Type | Description | Supported |
|-------|------|-------------|-----------|
| `features` | object | Features to install | ✓ (single-container only) |
| `overrideFeatureInstallOrder` | array | Feature install order | ✓ |

Features allow you to add tools and configurations to your container. dcx supports:
- **OCI Features**: From registries like `ghcr.io/devcontainers/features/go:1`
- **Local Features**: From local paths like `./features/myfeature`
- **HTTP Features**: From URLs (tarball)

Example with features:
```json
{
    "name": "Go Development",
    "image": "ubuntu:22.04",
    "features": {
        "ghcr.io/devcontainers/features/go:1": {
            "version": "1.21"
        },
        "ghcr.io/devcontainers/features/git:1": {}
    }
}
```

Feature caching: Features are cached in `~/.cache/dcx/features/` to avoid repeated downloads.

Features are supported for both single-container and compose-based configurations.

For compose configurations, dcx:
1. Parses the compose file to determine the base image
2. Builds the base image if needed (for services with `build:`)
3. Creates a derived image with features installed
4. Overrides the primary service image in the compose override file

### Host Requirements

| Field | Type | Description | Supported |
|-------|------|-------------|-----------|
| `hostRequirements.cpus` | number | Required CPUs | ✗ |
| `hostRequirements.memory` | string | Required memory | ✗ |
| `hostRequirements.storage` | string | Required storage | ✗ |
| `hostRequirements.gpu` | boolean/object | GPU requirements | ✗ |

### Customizations

| Field | Type | Description | Supported |
|-------|------|-------------|-----------|
| `customizations` | object | Tool customizations | ✗ |

## Variable Substitution

dcx supports these variables in string values:

| Variable | Description |
|----------|-------------|
| `${localWorkspaceFolder}` | Host workspace path |
| `${containerWorkspaceFolder}` | Container workspace path |
| `${localWorkspaceFolderBasename}` | Workspace folder name |
| `${localEnv:VAR}` | Host environment variable |
| `${containerEnv:VAR}` | Container environment variable |

## runArgs Mapping

When using compose plans, these `runArgs` are mapped to compose options:

| runArgs | Compose Field |
|---------|---------------|
| `--cap-add=X` | `cap_add: [X]` |
| `--cap-drop=X` | `cap_drop: [X]` |
| `--security-opt=X` | `security_opt: [X]` |
| `--privileged` | `privileged: true` |
| `--init` | `init: true` |
| `--shm-size=X` | `shm_size: X` |
| `--device=X` | `devices: [X]` |
| `--add-host=X` | `extra_hosts: [X]` |
| `--network=X` / `--net=X` | `network_mode: X` |
| `--ipc=X` | `ipc: X` |
| `--pid=X` | `pid: X` |
| `--tmpfs=X` | `tmpfs: [X]` |
| `--sysctl=X=Y` | `sysctls: {X: Y}` |
| `-p X:Y` / `--publish=X:Y` | `ports: [X:Y]` |

For single-container plans, these options are passed directly to the Docker API.

## SELinux Handling

On Linux systems with SELinux in enforcing mode, dcx automatically adds the `:Z` suffix to bind mounts for proper relabeling.

## SSH Agent Forwarding

dcx automatically forwards the SSH agent into the container when `SSH_AUTH_SOCK` is set on the host. Use `--no-agent` to disable this.

The SSH agent is available inside the container at `/ssh-agent/agent.sock`.
