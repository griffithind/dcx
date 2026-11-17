package devcontainer

import (
	"os"
	"path"
	"path/filepath"
)

// The workspace layout of image and Dockerfile plans follows the reference
// implementation (its --mount-workspace-git-root, on by default): the git
// repository that contains the workspace is bind-mounted at
// /workspaces/<repository folder>, and the default workspaceFolder is the
// workspace's place inside it. A workspace that is the repository root, or
// is not in a repository, is mounted itself. workspaceFolder only moves the
// working directory, never the mount.

// hostMountFolder returns the folder mounted as the workspace: the nearest
// folder at or above local that holds a .git directory or file (a worktree
// or submodule), as `git rev-parse --show-cdup` finds it, or local itself.
func hostMountFolder(local string) string {
	for dir := local; ; {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return local
		}
		dir = parent
	}
}

// containerMountFolder is where the host mount folder is mounted.
func containerMountFolder(hostFolder string) string {
	return path.Join("/workspaces", filepath.Base(hostFolder))
}

// DetermineContainerWorkspaceFolder computes the container workspace folder:
// workspaceFolder when set, "/" for compose (the compose file mounts the
// workspace), otherwise the workspace's place under the mounted repository.
func DetermineContainerWorkspaceFolder(cfg *DevContainerConfig, localWorkspace string) string {
	if cfg.WorkspaceFolder != "" {
		return cfg.WorkspaceFolder
	}
	if cfg.PlanType() == PlanTypeCompose {
		return "/"
	}
	root := hostMountFolder(localWorkspace)
	rel, err := filepath.Rel(root, localWorkspace)
	if err != nil {
		rel = "."
	}
	return path.Join(containerMountFolder(root), filepath.ToSlash(rel))
}

// resolveWorkspaceMount returns the workspace mount of an image or
// Dockerfile plan: workspaceMount when set (nil for ""), otherwise the bind
// of the host mount folder. Compose plans have none (the compose file
// defines it). Validate has rejected a workspaceMount that does not parse.
func resolveWorkspaceMount(cfg *DevContainerConfig, localWorkspace string) *Mount {
	if cfg.PlanType() == PlanTypeCompose {
		return nil
	}
	if spec := cfg.WorkspaceMount; spec != nil {
		if *spec == "" {
			return nil
		}
		m, err := ParseWorkspaceMount(*spec)
		if err != nil {
			return nil
		}
		return m
	}
	if localWorkspace == "" {
		return nil
	}
	root := hostMountFolder(localWorkspace)
	return &Mount{Type: "bind", Source: root, Target: containerMountFolder(root)}
}
