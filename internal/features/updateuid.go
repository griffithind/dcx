package features

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/griffithind/dcx/internal/config"
)

// updateUIDDockerfile is the Dockerfile template for updating user UID/GID.
// This matches the devcontainers/cli reference implementation.
// See: https://github.com/devcontainers/cli/blob/main/scripts/updateUID.Dockerfile
const updateUIDDockerfile = `# syntax=docker/dockerfile:1
# check=skip=InvalidDefaultArgInFrom
ARG BASE_IMAGE
FROM ${BASE_IMAGE}

USER root

ARG REMOTE_USER
ARG NEW_UID
ARG NEW_GID
SHELL ["/bin/sh", "-c"]
RUN eval $(sed -n "s/${REMOTE_USER}:[^:]*:\([^:]*\):\([^:]*\):[^:]*:\([^:]*\).*/OLD_UID=\1;OLD_GID=\2;HOME_FOLDER=\3/p" /etc/passwd); \
	eval $(sed -n "s/\([^:]*\):[^:]*:${NEW_UID}:.*/EXISTING_USER=\1/p" /etc/passwd); \
	eval $(sed -n "s/\([^:]*\):[^:]*:${NEW_GID}:.*/EXISTING_GROUP=\1/p" /etc/group); \
	if [ -z "$OLD_UID" ]; then \
		echo "Remote user not found in /etc/passwd ($REMOTE_USER)."; \
	elif [ "$OLD_UID" = "$NEW_UID" -a "$OLD_GID" = "$NEW_GID" ]; then \
		echo "UIDs and GIDs are the same ($NEW_UID:$NEW_GID)."; \
	elif [ "$OLD_UID" != "$NEW_UID" -a -n "$EXISTING_USER" ]; then \
		echo "User with UID exists ($EXISTING_USER=$NEW_UID)."; \
	else \
		if [ "$OLD_GID" != "$NEW_GID" -a -n "$EXISTING_GROUP" ]; then \
			echo "Group with GID exists ($EXISTING_GROUP=$NEW_GID)."; \
			NEW_GID="$OLD_GID"; \
		fi; \
		echo "Updating UID:GID from $OLD_UID:$OLD_GID to $NEW_UID:$NEW_GID."; \
		sed -i -e "s/\(${REMOTE_USER}:[^:]*:\)[^:]*:[^:]*/\1${NEW_UID}:${NEW_GID}/" /etc/passwd; \
		if [ "$OLD_GID" != "$NEW_GID" ]; then \
			sed -i -e "s/\([^:]*:[^:]*:\)${OLD_GID}:/\1${NEW_GID}:/" /etc/group; \
		fi; \
		chown -R $NEW_UID:$NEW_GID $HOME_FOLDER; \
	fi;

ARG IMAGE_USER
USER $IMAGE_USER
`

// BuildUpdateUIDImage builds an image with updated UID/GID for the remote user.
// This creates a new image layer on top of the base image with the user's UID/GID
// updated to match the host system. This is done at build time per the devcontainer spec.
//
// Parameters:
//   - baseImage: the image to build on top of (e.g., "mcr.microsoft.com/devcontainers/go:1")
//   - newTag: the tag for the resulting image
//   - remoteUser: the user whose UID/GID should be updated (e.g., "vscode")
//   - imageUser: the user the container should run as after UID update
//   - hostUID: the host user's UID to match
//   - hostGID: the host user's GID to match
func BuildUpdateUIDImage(ctx context.Context, baseImage, newTag, remoteUser, imageUser string, hostUID, hostGID int) error {
	// Validate inputs
	if baseImage == "" {
		return fmt.Errorf("baseImage is required for UID update")
	}
	if remoteUser == "" {
		return fmt.Errorf("remoteUser is required for UID update")
	}

	// Create temporary build directory
	tempBuildDir, err := os.MkdirTemp("", "dcx-updateuid-*")
	if err != nil {
		return fmt.Errorf("failed to create temp build directory: %w", err)
	}
	defer os.RemoveAll(tempBuildDir)

	// Write the Dockerfile
	dockerfilePath := filepath.Join(tempBuildDir, "Dockerfile.updateuid")
	if err := os.WriteFile(dockerfilePath, []byte(updateUIDDockerfile), 0644); err != nil {
		return fmt.Errorf("failed to write updateUID Dockerfile: %w", err)
	}

	// If imageUser is empty, default to remoteUser
	if imageUser == "" {
		imageUser = remoteUser
	}

	// Build the image with build args
	args := []string{
		"build",
		"-t", newTag,
		"-f", dockerfilePath,
		"--build-arg", "BASE_IMAGE=" + baseImage,
		"--build-arg", "REMOTE_USER=" + remoteUser,
		"--build-arg", "NEW_UID=" + strconv.Itoa(hostUID),
		"--build-arg", "NEW_GID=" + strconv.Itoa(hostGID),
		"--build-arg", "IMAGE_USER=" + imageUser,
		tempBuildDir,
	}

	cmd := execCommand(ctx, "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build updateUID image: %w", err)
	}

	return nil
}

// ShouldUpdateRemoteUserUID determines if we should update the remote user's UID.
// This is called during the build phase to decide whether to add the UID update layer.
//
// Returns true when:
//   - Platform is Linux or macOS (Darwin)
//   - Host user is not root (UID 0)
//   - updateRemoteUserUID is not explicitly disabled
//   - remoteUser is not "root"
func ShouldUpdateRemoteUserUID(cfg *config.DevcontainerConfig, remoteUser string, hostUID int) bool {
	// Skip on Windows (different file sharing semantics)
	if runtime.GOOS == "windows" {
		return false
	}

	// Skip if host is root
	if hostUID == 0 {
		return false
	}

	// Skip if remote user is root
	if remoteUser == "root" || remoteUser == "0" {
		return false
	}

	// If explicitly set in config, use that value
	if cfg != nil && cfg.UpdateRemoteUserUID != nil {
		return *cfg.UpdateRemoteUserUID
	}

	// Default to true on Linux and macOS
	return true
}
