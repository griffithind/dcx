package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	containerPkg "github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/service"
	sshContainer "github.com/griffithind/dcx/internal/ssh/container"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var sshCmd = &cobra.Command{
	Use:   "ssh [container-name]",
	Short: "SSH into container",
	Long: `SSH into a devcontainer environment.

Without arguments: shows connection info for current environment.
With --stdio: provides stdio transport (used by SSH ProxyCommand).
With --connect: connects directly via ssh.`,
	RunE: runSSH,
}

var (
	sshStdio   bool // Used by ProxyCommand in ~/.ssh/config
	sshConnect bool
)

func init() {
	sshCmd.Flags().BoolVar(&sshStdio, "stdio", false, "Stdio transport for SSH ProxyCommand")
	sshCmd.Flags().BoolVar(&sshConnect, "connect", false, "Connect directly via ssh")
	sshCmd.GroupID = "utilities"
	rootCmd.AddCommand(sshCmd)
}

func runSSH(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Get identifiers (needed for both modes)
	_, err := containerPkg.DockerClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	svc := service.NewDevContainerService(workspacePath, configPath, verbose)
	defer svc.Close()

	ids, err := svc.GetIdentifiers()
	if err != nil {
		return fmt.Errorf("failed to get identifiers: %w", err)
	}

	// --stdio mode: Called by SSH client via ProxyCommand
	if sshStdio {
		containerName := ""
		if len(args) > 0 {
			containerName = args[0]
		} else {
			// Try to find container from current directory
			containerName = ids.WorkspaceID
		}
		return runSSHStdio(ctx, containerName)
	}

	// Normal mode: Show connection info or connect
	if sshConnect {
		// Connect directly via ssh
		sshPath, err := exec.LookPath("ssh")
		if err != nil {
			return fmt.Errorf("ssh not found in PATH")
		}
		return syscall.Exec(sshPath, []string{"ssh", ids.SSHHost}, os.Environ())
	}

	// Print connection info
	ui.Printf("ssh %s", ids.SSHHost)
	return nil
}

// runSSHStdio implements the stdio transport for ProxyCommand.
// This is called by the SSH client when using:
//
//	ProxyCommand dcx ssh --stdio <container-name>
func runSSHStdio(ctx context.Context, containerName string) error {
	// Initialize Docker client (uses singleton)
	docker, err := containerPkg.DockerClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Initialize state manager
	stateMgr := state.NewStateManager(docker)

	// Look up container by name
	containerInfo, err := stateMgr.FindContainerByName(ctx, containerName)
	if err != nil {
		return fmt.Errorf("failed to find container: %w", err)
	}

	if containerInfo == nil {
		return fmt.Errorf("container not found: %s", containerName)
	}

	if !containerInfo.Running {
		return fmt.Errorf("container is not running: %s", containerName)
	}

	// Get workspace path from container labels (set during dcx up)
	// This ensures SSH works regardless of the current directory
	wsPath := containerInfo.Labels.WorkspacePath
	if wsPath == "" {
		wsPath = workspacePath // Fallback to current directory
	}

	// Load config to get user and workspace folder
	cfg, _, _ := devcontainer.Load(wsPath, configPath)

	// Determine user and workdir
	var user, workDir string
	if cfg != nil {
		user = cfg.RemoteUser
		if user == "" {
			user = cfg.ContainerUser
		}
		if user != "" {
			user = devcontainer.Substitute(user, &devcontainer.SubstitutionContext{
				LocalWorkspaceFolder: wsPath,
			})
		}
		workDir = devcontainer.DetermineContainerWorkspaceFolder(cfg, wsPath)
	}

	// Default values
	if user == "" {
		user = "root"
	}
	if workDir == "" {
		workDir = "/workspace"
	}

	// Deploy dcx-agent binary to container if needed
	binaryPath := sshContainer.GetContainerBinaryPath()
	if err := sshContainer.DeployToContainer(ctx, containerInfo.Name, binaryPath); err != nil {
		return fmt.Errorf("failed to deploy SSH server: %w", err)
	}

	// Run exec with SSH server (stdio mode) using Docker SDK
	// Run as the target user so the SSH server process has the correct identity
	cmd := []string{
		binaryPath, "ssh-server",
		"--user", user,
		"--workdir", workDir,
	}

	// Build remoteEnv for SSH server process
	// The SSH server inherits these env vars, and sessions spawned will have them
	var env []string
	if cfg != nil {
		for k, v := range cfg.RemoteEnv {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	exitCode, err := containerPkg.Exec(ctx, containerPkg.ExecConfig{
		ContainerID: containerInfo.ID,
		Cmd:         cmd,
		User:        user,
		Env:         env,
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	})
	if err != nil {
		return fmt.Errorf("exec failed: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("ssh-server exited with code %d", exitCode)
	}
	return nil
}
