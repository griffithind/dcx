package service

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
)

// updateRemoteUserUID updates the UID/GID of the remoteUser inside the container
// to match the host user's UID/GID. This is important on Linux for proper file
// permissions on bind mounts.
//
// This only runs when:
// - updateRemoteUserUID is explicitly true, OR
// - updateRemoteUserUID is not set (nil) and we're on Linux (default behavior)
//
// It does NOT run when:
// - updateRemoteUserUID is explicitly false
// - We're not on Linux (macOS/Windows use different file sharing mechanisms)
// - No remoteUser is specified
// - remoteUser is "root" (UID 0)
func (s *EnvironmentService) updateRemoteUserUID(ctx context.Context, containerID string, cfg *config.DevcontainerConfig) error {
	// Check if we should update the UID
	if !shouldUpdateRemoteUserUID(cfg) {
		return nil
	}

	// Get remoteUser (fall back to containerUser)
	remoteUser := cfg.RemoteUser
	if remoteUser == "" {
		remoteUser = cfg.ContainerUser
	}
	if remoteUser == "" {
		return nil // No user to update
	}

	// Apply variable substitution
	remoteUser = config.Substitute(remoteUser, &config.SubstitutionContext{
		LocalWorkspaceFolder: s.workspacePath,
	})

	// Don't update root user
	if remoteUser == "root" {
		return nil
	}

	// Get host UID/GID
	hostUID := os.Getuid()
	hostGID := os.Getgid()

	// On Linux, UID 0 means root - no need to update
	if hostUID == 0 {
		return nil
	}

	if s.verbose {
		fmt.Printf("Updating %s UID/GID to %d:%d...\n", remoteUser, hostUID, hostGID)
	}

	// Run as root to update the user's UID/GID
	// First, check if the user exists and get their current UID/GID
	checkCmd := fmt.Sprintf("id -u %s 2>/dev/null && id -g %s 2>/dev/null", remoteUser, remoteUser)
	exitCode, err := s.dockerClient.Exec(ctx, containerID, docker.ExecConfig{
		Cmd:  []string{"/bin/sh", "-c", checkCmd},
		User: "root",
	})
	if err != nil || exitCode != 0 {
		// User doesn't exist in container, skip
		if s.verbose {
			fmt.Printf("User %s not found in container, skipping UID update\n", remoteUser)
		}
		return nil
	}

	// Update UID/GID using usermod and groupmod
	// We need to:
	// 1. Get the user's primary group name
	// 2. Update the group's GID first (if different)
	// 3. Update the user's UID (if different)
	// 4. Fix ownership of the user's home directory

	updateScript := fmt.Sprintf(`
set -e

TARGET_USER="%s"
TARGET_UID=%d
TARGET_GID=%d

# Get current values
CURRENT_UID=$(id -u "$TARGET_USER")
CURRENT_GID=$(id -g "$TARGET_USER")
USER_GROUP=$(id -gn "$TARGET_USER")
USER_HOME=$(getent passwd "$TARGET_USER" | cut -d: -f6)

# Update GID if different
if [ "$CURRENT_GID" != "$TARGET_GID" ]; then
    # Check if target GID is already in use by another group
    EXISTING_GROUP=$(getent group "$TARGET_GID" 2>/dev/null | cut -d: -f1 || true)
    if [ -n "$EXISTING_GROUP" ] && [ "$EXISTING_GROUP" != "$USER_GROUP" ]; then
        # Target GID is used by another group, we need to change that group first
        groupmod -g 65534 "$EXISTING_GROUP" 2>/dev/null || true
    fi
    groupmod -g "$TARGET_GID" "$USER_GROUP" 2>/dev/null || true
fi

# Update UID if different
if [ "$CURRENT_UID" != "$TARGET_UID" ]; then
    # Check if target UID is already in use by another user
    EXISTING_USER=$(getent passwd "$TARGET_UID" 2>/dev/null | cut -d: -f1 || true)
    if [ -n "$EXISTING_USER" ] && [ "$EXISTING_USER" != "$TARGET_USER" ]; then
        # Target UID is used by another user, we need to change that user first
        usermod -u 65534 "$EXISTING_USER" 2>/dev/null || true
    fi
    usermod -u "$TARGET_UID" "$TARGET_USER" 2>/dev/null || true
fi

# Fix ownership of home directory if it exists
if [ -d "$USER_HOME" ]; then
    chown -R "$TARGET_UID:$TARGET_GID" "$USER_HOME" 2>/dev/null || true
fi
`, remoteUser, hostUID, hostGID)

	exitCode, err = s.dockerClient.Exec(ctx, containerID, docker.ExecConfig{
		Cmd:  []string{"/bin/sh", "-c", updateScript},
		User: "root",
	})
	if err != nil {
		return fmt.Errorf("failed to update user UID/GID: %w", err)
	}
	if exitCode != 0 {
		// Non-fatal - some containers may not have usermod/groupmod
		if s.verbose {
			fmt.Printf("Warning: Could not update user UID/GID (exit code %d)\n", exitCode)
		}
	}

	return nil
}

// shouldUpdateRemoteUserUID determines if we should update the remote user's UID.
func shouldUpdateRemoteUserUID(cfg *config.DevcontainerConfig) bool {
	// Only applicable on Linux
	if runtime.GOOS != "linux" {
		return false
	}

	// If explicitly set, use that value
	if cfg.UpdateRemoteUserUID != nil {
		return *cfg.UpdateRemoteUserUID
	}

	// Default to true on Linux (devcontainer spec default)
	return true
}

// getRemoteUser returns the effective remote user from the config.
func getRemoteUser(cfg *config.DevcontainerConfig, workspacePath string) string {
	user := cfg.RemoteUser
	if user == "" {
		user = cfg.ContainerUser
	}
	if user == "" {
		return ""
	}
	return config.Substitute(user, &config.SubstitutionContext{
		LocalWorkspaceFolder: workspacePath,
	})
}

// isRootUser checks if a username represents the root user.
func isRootUser(username string) bool {
	return username == "root" || username == "0"
}

// getUserIDInfo gets the current UID/GID of a user inside a container.
func (s *EnvironmentService) getUserIDInfo(ctx context.Context, containerID, username string) (uid, gid int, err error) {
	// Get UID
	cmd := fmt.Sprintf("id -u %s", username)
	output := &strings.Builder{}
	_, err = s.dockerClient.Exec(ctx, containerID, docker.ExecConfig{
		Cmd:    []string{"/bin/sh", "-c", cmd},
		User:   "root",
		Stdout: output,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get user UID: %w", err)
	}

	// Parse UID
	uidStr := strings.TrimSpace(output.String())
	if _, err := fmt.Sscanf(uidStr, "%d", &uid); err != nil {
		return 0, 0, fmt.Errorf("failed to parse UID: %w", err)
	}

	// Get GID
	output.Reset()
	cmd = fmt.Sprintf("id -g %s", username)
	_, err = s.dockerClient.Exec(ctx, containerID, docker.ExecConfig{
		Cmd:    []string{"/bin/sh", "-c", cmd},
		User:   "root",
		Stdout: output,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get user GID: %w", err)
	}

	// Parse GID
	gidStr := strings.TrimSpace(output.String())
	if _, err := fmt.Sscanf(gidStr, "%d", &gid); err != nil {
		return 0, 0, fmt.Errorf("failed to parse GID: %w", err)
	}

	return uid, gid, nil
}
