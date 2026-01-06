# Command Reference

## Global Flags

These flags apply to all commands:

| Flag | Short | Description |
|------|-------|-------------|
| `--workspace` | `-w` | Workspace directory (default: current directory) |
| `--config` | `-c` | Path to devcontainer.json |
| `--verbose` | `-v` | Enable verbose output |
| `--version` | | Show version |
| `--help` | `-h` | Show help |

## Commands

### dcx up

Start the devcontainer environment, building if necessary.

```bash
dcx up [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--recreate` | Force recreate containers |
| `--rebuild` | Force rebuild images |
| `--no-agent` | Disable SSH agent forwarding |

**Behavior:**
- Parses devcontainer.json configuration
- Generates compose override with dcx labels
- Starts SSH agent proxy (unless `--no-agent`)
- Runs `docker compose up -d`
- Runs lifecycle hooks (onCreate, postCreate, postStart)

**Examples:**
```bash
# Start environment
dcx up

# Force rebuild
dcx up --rebuild

# Without SSH forwarding
dcx up --no-agent

# Verbose output
dcx up -v
```

**Network Required:** Yes (may pull images)

---

### dcx start

Start existing containers without rebuilding.

```bash
dcx start
```

**Behavior:**
- Only starts containers that already exist
- Never pulls images or rebuilds
- Fails if environment doesn't exist

**Examples:**
```bash
# Start stopped containers
dcx start

# In a specific workspace
dcx start -w /path/to/project
```

**Network Required:** No (offline-safe)

---

### dcx stop

Stop running containers without removing them.

```bash
dcx stop
```

**Behavior:**
- Stops containers managed by dcx
- Preserves container state and volumes
- Can be restarted with `dcx start`

**Examples:**
```bash
dcx stop
```

**Network Required:** No (offline-safe)

---

### dcx down

Stop and remove containers.

```bash
dcx down [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--volumes` | Also remove named volumes |
| `--remove-orphans` | Remove containers not in compose file |

**Behavior:**
- Stops containers if running
- Removes containers
- Optionally removes volumes

**Examples:**
```bash
# Remove containers
dcx down

# Remove containers and volumes
dcx down --volumes

# Clean everything
dcx down --volumes --remove-orphans
```

**Network Required:** No (offline-safe)

---

### dcx exec

Run a command in the running container.

```bash
dcx exec [--no-agent] -- <command> [args...]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--no-agent` | Disable SSH agent forwarding |

**Behavior:**
- Executes command in primary container
- Returns command exit code
- Requires environment to be running
- SSH agent forwarding enabled by default (if available)

**Examples:**
```bash
# Run a command
dcx exec -- npm install

# Run with arguments
dcx exec -- npm run build --production

# List files
dcx exec -- ls -la /workspace

# Without SSH agent forwarding
dcx exec --no-agent -- git status
```

**Network Required:** No (offline-safe)

---

### dcx shell

Open an interactive shell in the container.

```bash
dcx shell [--no-agent]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--no-agent` | Disable SSH agent forwarding |

**Behavior:**
- Opens interactive shell in primary container
- Uses `/bin/bash` if available, otherwise `/bin/sh`
- Allocates TTY for interactive use
- SSH agent forwarding enabled by default (if available)

**Examples:**
```bash
# Open shell
dcx shell

# Without SSH agent forwarding
dcx shell --no-agent
```

**Network Required:** No (offline-safe)

---

### dcx build

Build the devcontainer images without starting containers.

```bash
dcx build [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--no-cache` | Build without using cache |

**Behavior:**
- For compose-based configs: runs `docker compose build`
- For image-based configs: pulls the image from registry
- For Dockerfile-based configs: builds the image

**Examples:**
```bash
# Build images
dcx build

# Force rebuild without cache
dcx build --no-cache
```

**Network Required:** Yes (may pull images)

---

### dcx status

Show current environment status.

```bash
dcx status
```

**Output:**
```
Workspace:  /home/user/project
Env Key:    abcd1234efgh
State:      RUNNING

Primary Container:
  ID:       abc123def456
  Name:     dcx_abcd1234efgh-app-1
  Status:   Up 5 minutes
  Config:   a1b2c3d4e5f6
```

**States:**
| State | Description |
|-------|-------------|
| ABSENT | No containers exist |
| CREATED | Containers exist, not running |
| RUNNING | Primary container running |
| STALE | Config has changed |
| BROKEN | Inconsistent state |

**Network Required:** No (offline-safe)

---

### dcx doctor

Check system requirements and diagnose issues.

```bash
dcx doctor
```

**Checks:**
- Docker daemon connectivity
- Docker Compose availability
- SSH agent availability
- SELinux status (Linux only)

**Output:**
```
dcx doctor
==========

[✓] Docker: version 24.0.7
[✓] Docker Compose: version 2.23.0
[✓] SSH Agent: available at /run/user/1000/ssh-agent.sock
[✓] SELinux: enforcing (will use :Z bind mount option)

All checks passed!
```

**Network Required:** Partial (some checks may need network)

---

### dcx upgrade

Upgrade dcx to the latest version.

```bash
dcx upgrade
```

**Behavior:**
- Checks GitHub releases for latest version
- Compares with current version
- Downloads appropriate binary for your platform
- Replaces current executable in-place

**Examples:**
```bash
# Check current version first
dcx --version

# Upgrade to latest
dcx upgrade
```

**Output:**
```
Current version: v0.1.0
Latest version:  v0.2.0
Downloading dcx-darwin-arm64...
Installing to /Users/you/.local/bin/dcx...
Successfully upgraded to v0.2.0!
Release notes: https://github.com/griffithind/dcx/releases/tag/v0.2.0
```

**Network Required:** Yes

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Configuration error |
| 3 | Docker error |
| 4 | State error |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `SSH_AUTH_SOCK` | SSH agent socket (for forwarding) |
| `DOCKER_HOST` | Docker daemon address |
| `XDG_RUNTIME_DIR` | Runtime directory (Linux, for SSH proxy) |

## Offline-Safe Commands

These commands work without network access:

- `dcx start`
- `dcx stop`
- `dcx exec`
- `dcx shell`
- `dcx down`
- `dcx status`

These commands may require network:

- `dcx up` (may pull images)
- `dcx build` (may pull base images)
- `dcx doctor` (some checks)
- `dcx upgrade` (downloads from GitHub)
