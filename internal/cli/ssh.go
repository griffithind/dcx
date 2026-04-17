package cli

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	containerPkg "github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var sshCmd = &cobra.Command{
	Use:   "ssh",
	Short: "SSH into the container",
	Long: `SSH into the devcontainer for the current workspace.

With no flags, prints the ssh command to use. With --connect, execs ssh
directly so the running process becomes the ssh session.`,
	RunE: runSSH,
}

var sshConnect bool

func init() {
	sshCmd.Flags().BoolVar(&sshConnect, "connect", false, "Exec ssh directly instead of printing the command")
	sshCmd.GroupID = "utilities"
	rootCmd.AddCommand(sshCmd)
}

func runSSH(cmd *cobra.Command, args []string) error {
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

	if sshConnect {
		sshPath, err := exec.LookPath("ssh")
		if err != nil {
			return fmt.Errorf("ssh not found in PATH")
		}
		return syscall.Exec(sshPath, []string{"ssh", ids.SSHHost}, os.Environ())
	}

	ui.Printf("ssh %s", ids.SSHHost)
	return nil
}
