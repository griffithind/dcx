# Command Reference

## Global Flags

These flags apply to all commands:

| Flag | Short | Description |
|------|-------|-------------|
| `--workspace` | `-w` | Workspace directory (default: current directory) |
| `--config` | `-c` | Path to devcontainer.json |
| `--verbose` | `-v` | Enable verbose output |
| `--quiet` | `-q` | Minimal output (errors only) |
| `--json` | | Output as JSON |
| `--no-color` | | Disable colored output |
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
| `--pull` | Force re-fetch remote features |
| `--no-agent` | Disable SSH agent forwarding |
| `--ssh` | Enable SSH server access |

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

### dcx stop

Stop running containers without removing them.

```bash
dcx stop [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--force`, `-f` | Force stop even if shutdownAction is "none" |

**Behavior:**
- Stops containers managed by dcx
- Preserves container state and volumes
- Can be restarted with `dcx up`
- Respects `shutdownAction` setting unless `--force`

**Examples:**
```bash
# Stop containers
dcx stop

# Force stop
dcx stop --force
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
| `--pull` | Force re-fetch remote features |

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
dcx status [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--detailed`, `-d` | Show detailed environment information |

**Output:**
```
Workspace:  /home/user/project
Workspace ID: abcd1234efgh
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
dcx doctor [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--config` | Only check configuration (skip system checks) |
| `--system` | Only check system requirements (skip config checks) |

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

### dcx restart

Stop and start containers without rebuilding.

```bash
dcx restart [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--force`, `-f` | Force restart even if shutdownAction is "none" |
| `--rebuild` | Perform full rebuild instead of just restart |

**Behavior:**
- Stops running containers
- Starts containers again
- Respects devcontainer.json `shutdownAction` setting unless `--force`
- Does not rebuild images unless `--rebuild` is specified

**Examples:**
```bash
# Restart containers
dcx restart

# Force restart
dcx restart --force

# Restart with rebuild
dcx restart --rebuild
```

**Network Required:** No (unless `--rebuild`)

---

### dcx run

Execute command shortcuts defined in `.devcontainer/dcx.json`.

```bash
dcx run [--no-agent] [--list] <shortcut> [args...]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--no-agent` | Disable SSH agent forwarding |
| `--list`, `-l` | List available shortcuts |

**Behavior:**
- Reads shortcuts from `.devcontainer/dcx.json`
- Resolves shortcut arguments and prefix expansion
- Executes command in the running container
- Supports TTY detection for interactive commands

**dcx.json format:**
```json
{
  "shortcuts": {
    "test": "npm test",
    "lint": "npm run lint",
    "build": {
      "command": "npm run build",
      "prefix": "--"
    }
  }
}
```

**Examples:**
```bash
# List available shortcuts
dcx run --list

# Run a shortcut
dcx run test

# Run with arguments
dcx run build --production
```

**Network Required:** No (offline-safe)

---

### dcx list

List all dcx-managed devcontainer environments.

```bash
dcx list [flags]
```

**Aliases:** `ls`, `ps`

**Flags:**
| Flag | Description |
|------|-------------|
| `--all` | Show all environments including stopped ones |

**Behavior:**
- Groups containers by environment (workspace)
- Shows container state (running, stopped, broken, stale)
- Marks primary containers with asterisk (*)
- By default shows only running environments

**Examples:**
```bash
# List running environments
dcx list

# List all environments
dcx list --all

# Using alias
dcx ls
dcx ps
```

**Network Required:** No (offline-safe)

---

### dcx logs

View logs from the devcontainer's primary container.

```bash
dcx logs [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--follow`, `-f` | Stream logs in real-time |
| `--tail` | Number of lines from end (default: 100, or "all") |
| `--timestamps`, `-t` | Include timestamps in output |

**Behavior:**
- Streams logs from primary container
- Default shows last 100 lines
- Can follow in real-time with `-f`

**Examples:**
```bash
# View recent logs
dcx logs

# Follow logs in real-time
dcx logs -f

# Show last 50 lines with timestamps
dcx logs --tail 50 -t

# Show all logs
dcx logs --tail all
```

**Network Required:** No (offline-safe)

---

### dcx config

Show resolved devcontainer.json configuration.

```bash
dcx config [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--validate` | Only validate, no output |
| `--raw` | Show original without variable substitution |

**Behavior:**
- Parses and validates devcontainer.json
- Performs variable substitution (${localEnv:VAR}, etc.)
- Outputs resolved configuration as JSON
- Shows workspace ID and config hash

**Examples:**
```bash
# Show resolved config
dcx config

# Validate only
dcx config --validate

# Show raw config
dcx config --raw
```

**Network Required:** No (offline-safe)

---

### dcx clean

Remove orphaned Docker resources created by dcx.

```bash
dcx clean [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--all` | Also clean dangling images |
| `--dangling` | Only clean dangling images |
| `--dry-run` | Show what would be cleaned without removing |

**Behavior:**
- Removes derived images (dcx-derived/*)
- Reports space reclaimed
- Safe to run without affecting running environments

**Examples:**
```bash
# Preview cleanup
dcx clean --dry-run

# Clean derived images
dcx clean

# Clean all dcx images including dangling
dcx clean --all
```

**Network Required:** No (offline-safe)

---

### dcx ssh

SSH access to the devcontainer environment.

```bash
dcx ssh [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--stdio` | Stdio transport (for ProxyCommand) |
| `--connect` | Connect directly via ssh command |

**Behavior:**
- Without flags: shows SSH connection info
- With `--connect`: connects directly via SSH
- With `--stdio`: acts as ProxyCommand transport
- Deploys dcx binary to container as SSH server

**SSH Config Integration:**
```
Host myproject.dcx
  ProxyCommand dcx ssh --stdio -w /path/to/project
  User vscode
```

**Examples:**
```bash
# Show connection info
dcx ssh

# Connect directly
dcx ssh --connect
```

**Network Required:** No (offline-safe)

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

- `dcx stop`
- `dcx exec`
- `dcx shell`
- `dcx run`
- `dcx down`
- `dcx status`
- `dcx list`
- `dcx logs`
- `dcx config`
- `dcx clean`
- `dcx ssh`
- `dcx up` (when containers already exist and are up to date)

These commands may require network:

- `dcx up` (may pull images)
- `dcx build` (may pull base images)
- `dcx restart --rebuild` (may pull images)
- `dcx doctor` (some checks)
- `dcx upgrade` (downloads from GitHub)
