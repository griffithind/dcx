package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/griffithind/dcx/internal/config"
	ctr "github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/ssh/container"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/griffithind/dcx/internal/version"
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
	dockerClient, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	svc := service.NewEnvironmentService(dockerClient, workspacePath, configPath, verbose)
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
	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Initialize state manager
	stateMgr := ctr.NewManager(dockerClient)

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
	cfg, _, _ := config.Load(wsPath, configPath)

	// Determine user and workdir
	var user, workDir string
	if cfg != nil {
		user = cfg.RemoteUser
		if user == "" {
			user = cfg.ContainerUser
		}
		if user != "" {
			user = config.Substitute(user, &config.SubstitutionContext{
				LocalWorkspaceFolder: wsPath,
			})
		}
		workDir = config.DetermineContainerWorkspaceFolder(cfg, wsPath)
	}

	// Default values
	if user == "" {
		user = "root"
	}
	if workDir == "" {
		workDir = "/workspace"
	}

	// Deploy dcx binary to container if needed
	binaryPath := fmt.Sprintf("/tmp/dcx-%s", version.Version)
	if err := container.DeployToContainer(ctx, containerInfo.Name, binaryPath); err != nil {
		return fmt.Errorf("failed to deploy SSH server: %w", err)
	}

	// Run docker exec with SSH server (stdio mode)
	// Run as the target user so the SSH server process has the correct identity
	dockerArgs := []string{
		"exec", "-i",
		"-u", user,
		containerInfo.Name,
		binaryPath, "ssh-server",
		"--user", user,
		"--workdir", workDir,
	}

	dockerCmd := exec.Command("docker", dockerArgs...)

	// Pipe stdin/stdout through docker exec
	dockerCmd.Stdin = os.Stdin
	dockerCmd.Stdout = os.Stdout
	dockerCmd.Stderr = os.Stderr

	return dockerCmd.Run()
}
