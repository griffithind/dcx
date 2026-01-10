# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

```bash
make build          # Build dcx with embedded agent binaries (builds agent first)
make build-agent    # Build only the dcx-agent Linux binaries (amd64/arm64)
make test           # Run unit tests (alias for test-unit)
make test-e2e       # Run end-to-end tests (requires Docker)
make lint           # Run golangci-lint
```

Run a single test:
```bash
go test -v -run TestShouldUpdateRemoteUserUID ./internal/build/...
go test -v -tags=e2e -run TestSingleImageBasedE2E ./test/e2e/...
```

## Architecture Overview

DCX is a single-binary CLI for running devcontainers with built-in SSH support. It embeds a minimal `dcx-agent` binary (gzip-compressed) that gets deployed to containers for SSH server and agent proxy functionality.

### Package Structure

```
cmd/dcx/           → Main CLI entry point
cmd/dcx-agent/     → Minimal agent binary for container SSH/proxy
agent-embed.go     → Embedded agent binaries via //go:embed

internal/
  cli/             → Cobra commands (up, down, exec, shell, etc.)
  service/         → DevContainerService - main business logic layer
  devcontainer/    → Config types, parsing, variable substitution, resolution
  container/       → DockerClient, UnifiedRuntime (runtime abstraction)
  build/           → SDKBuilder for image building, feature installation
  features/        → Feature resolution, dependency ordering, Dockerfile generation
  state/           → Container state tracking via Docker labels
  lifecycle/       → Hook execution (onCreateCommand, postStartCommand, etc.)
  ssh/             → SSH agent detection, container deployment, host config
  compose/         → Docker Compose override generation
  shortcuts/       → Shortcut resolution from customizations.dcx
```

### Key Data Flow

1. **Config Resolution**: `devcontainer.Load()` → `builder.Build()` → `ResolvedDevContainer`
   - Parses `devcontainer.json` (with JSONC support for comments)
   - Resolves features with recursive dependency resolution
   - Performs variable substitution (`${localEnv:VAR}`, `${containerWorkspaceFolder}`, etc.)
   - Computes configuration hashes for change detection

2. **Container Lifecycle**: `DevContainerService.Up()` orchestrates:
   - State detection via Docker labels (`StateManager`)
   - Plan action determination (Create, Start, Recreate, Skip)
   - Image building with features (`SDKBuilder`)
   - Container creation via `UnifiedRuntime`
   - Agent deployment and lifecycle hook execution

3. **Plan Types**: The `UnifiedRuntime` handles three plan types uniformly:
   - `ImagePlan`: Uses pre-built image directly
   - `DockerfilePlan`: Builds from Dockerfile
   - `ComposePlan`: Uses docker-compose with service override

### State Management

Container state is stored in Docker labels (offline-safe, no separate database):
- States: `Absent`, `Created`, `Running`, `Stopped`, `Stale`, `Broken`
- Labels include config hash, project name, workspace ID, build method
- Hash comparison detects config changes requiring rebuild

### Agent Binary Embedding

The main `dcx` binary embeds gzip-compressed Linux agent binaries:
- Built via `make build-agent` before main build
- Embedded in `agent-embed.go` using `//go:embed bin/dcx-agent-linux-*.gz`
- Decompressed lazily at runtime via `sync.Once`
- Deployed to containers at `/tmp/dcx-agent`

### SSH Integration

The `dcx-agent` binary provides:
- `ssh-server`: SSH server in stdio mode for editor connections
- `ssh-agent-proxy`: TCP↔Unix socket proxy for SSH agent forwarding

When `dcx up --ssh` is used:
1. Agent binary deployed to container
2. SSH server configured with container user/shell
3. Host SSH config updated for `projectname.dcx` access

## DCX Customizations

DCX-specific settings are stored in `customizations.dcx` within devcontainer.json:

```json
{
  "name": "my-project",
  "image": "ubuntu",
  "customizations": {
    "dcx": {
      "up": {
        "ssh": true,
        "noAgent": false
      },
      "shortcuts": {
        "r": {"prefix": "rails", "passArgs": true},
        "rw": "bin/jobs --skip-recurring"
      }
    }
  }
}
```

### Project Naming

The `name` field in devcontainer.json is used for:
- Container/Compose project naming (sanitized)
- SSH host (`<sanitized-name>.dcx`)
- Display name in `dcx status`

If no `name` is provided, the workspace ID (hash-based) is used as fallback.

## Key Patterns

- **CLI → Service → Runtime**: Commands delegate to `DevContainerService`, which uses `UnifiedRuntime`
- **Strategy in UnifiedRuntime**: Single implementation handles all plan types internally
- **Labels for State**: All state persisted in Docker container labels
- **Feature Resolution**: Recursive dependency resolution with topological sort
- **Variable Substitution**: Centralized in `devcontainer/substitute.go`

## Testing

- Unit tests: `internal/*/` packages
- E2E tests: `test/e2e/` (require Docker, use build tags `e2e`)
- Conformance tests: `test/conformance/` (devcontainer spec compliance)
- Test helpers: `test/helpers/` (fixtures, Docker utilities)
