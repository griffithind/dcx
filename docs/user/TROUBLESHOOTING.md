# Troubleshooting Guide

This document covers common issues and their solutions when using dcx.

## Environment States

dcx tracks the state of your devcontainer environment. Understanding these states helps diagnose issues.

### State Definitions

| State | Description | Recovery |
|-------|-------------|----------|
| `ABSENT` | No containers exist for this environment | Run `dcx up` to create |
| `CREATED` | Container exists but is stopped | Run `dcx start` to start |
| `RUNNING` | Container is running normally | No action needed |
| `STALE` | Configuration has changed since container was created | Run `dcx up --rebuild` to apply changes |
| `BROKEN` | Environment is in an inconsistent state | Run `dcx down` then `dcx up` |

### Checking State

```bash
dcx status
```

This shows the current state, container info, and recommended recovery actions.

## Common Issues

### "Container is not running"

**Symptoms:** Commands like `dcx exec` or `dcx shell` fail with this error.

**Cause:** The container exists but is stopped.

**Solution:**
```bash
dcx start
```

### "No container found for this environment"

**Symptoms:** Most commands fail with this error.

**Cause:** No container has been created yet.

**Solution:**
```bash
dcx up
```

### "Configuration has changed, rebuild required"

**Symptoms:** `dcx start` fails with this error.

**Cause:** You've modified devcontainer.json since the container was created.

**Solution:**
```bash
dcx up --rebuild
# or
dcx down && dcx up
```

### "Environment is in an inconsistent state"

**Symptoms:** Operations fail unexpectedly, state shows as BROKEN.

**Cause:** Something went wrong during a previous operation, or containers were manually modified.

**Solution:**
```bash
dcx down
dcx up
```

### Docker Connection Issues

**Symptoms:** "Cannot connect to Docker daemon" or similar errors.

**Cause:** Docker daemon is not running or not accessible.

**Solutions:**
1. Start Docker Desktop (macOS/Windows)
2. Start Docker service: `sudo systemctl start docker` (Linux)
3. Check Docker socket permissions
4. Run `dcx doctor` to diagnose

### Compose File Not Found

**Symptoms:** "compose file not found" error during `dcx up`.

**Cause:** The dockerComposeFile path in devcontainer.json is incorrect.

**Solution:**
- Verify the path is relative to devcontainer.json location
- Check file exists: `ls -la .devcontainer/`

### Feature Installation Failures

**Symptoms:** Build fails during feature installation.

**Cause:** Feature script error, network issues, or incompatible base image.

**Solutions:**
1. Check feature compatibility with your base image
2. Try with `--verbose` to see detailed output: `dcx up --verbose`
3. Clear feature cache: `rm -rf ~/.cache/dcx/features/`
4. Try without the problematic feature to isolate the issue

### SSH Agent Not Working

**Symptoms:** SSH operations inside container fail, `ssh-add -l` shows no identities.

**Cause:** SSH agent forwarding not configured or socket not mounted.

**Solutions:**
1. Ensure SSH agent is running on host: `ssh-add -l`
2. Check SSH_AUTH_SOCK is set: `echo $SSH_AUTH_SOCK`
3. Remove `--no-agent` flag if used
4. Restart dcx: `dcx down && dcx up`

### Port Conflicts

**Symptoms:** "port is already allocated" error.

**Cause:** Another process is using the port.

**Solutions:**
1. Find process using port: `lsof -i :8080`
2. Stop conflicting process or change the port in devcontainer.json
3. Use different host port: `"forwardPorts": ["8081:8080"]`

### SELinux Permission Denied

**Symptoms:** Permission denied errors when accessing mounted files.

**Cause:** SELinux blocking container access to host files.

**Solutions:**
1. dcx should automatically add `:Z` suffix on SELinux systems
2. Check SELinux mode: `getenforce`
3. Temporarily set permissive: `sudo setenforce 0` (for testing only)

## Diagnostic Commands

### Check Docker Status
```bash
dcx doctor
```

### View Container Logs
```bash
docker logs dcx_<env_key>
```

### Inspect Container
```bash
docker inspect dcx_<env_key>
```

### List DCX Containers
```bash
docker ps -a --filter "label=io.github.dcx.managed=true"
```

### Force Cleanup
If normal cleanup fails:
```bash
# Stop all dcx containers
docker stop $(docker ps -q --filter "label=io.github.dcx.managed=true")

# Remove all dcx containers
docker rm $(docker ps -aq --filter "label=io.github.dcx.managed=true")
```

## Getting Help

If you're still having issues:
1. Run with `--verbose` flag to get detailed output
2. Check the GitHub issues: https://github.com/anthropics/claude-code/issues
3. Include your devcontainer.json (with sensitive data removed) when reporting issues
