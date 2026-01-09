package cli

import (
	"os"

	"github.com/griffithind/dcx/internal/ssh/container"
	"github.com/spf13/cobra"
)

var sshServerCmd = &cobra.Command{
	Use:    "ssh-server",
	Short:  "Run SSH server (internal, runs in container)",
	Hidden: true,
	RunE:   runSSHServer,
}

var (
	sshServerUser    string
	sshServerWorkDir string
	sshServerShell   string
)

func init() {
	sshServerCmd.Flags().StringVar(&sshServerUser, "user", "", "User to run as")
	sshServerCmd.Flags().StringVar(&sshServerWorkDir, "workdir", "/workspace", "Working directory")
	sshServerCmd.Flags().StringVar(&sshServerShell, "shell", "", "Shell to use (auto-detected if empty)")
	sshServerCmd.Hidden = true
	rootCmd.AddCommand(sshServerCmd)
}

func runSSHServer(cmd *cobra.Command, args []string) error {
	shell := sshServerShell
	if shell == "" {
		shell = detectShell()
	}

	hostKeyPath := "/tmp/dcx-ssh-hostkey"
	server, err := container.NewServer(sshServerUser, shell, sshServerWorkDir, hostKeyPath)
	if err != nil {
		return err
	}

	// Always serve on stdin/stdout (stdio mode only)
	return server.Serve()
}

// detectShell finds an available shell in the container.
func detectShell() string {
	shells := []string{"/bin/bash", "/bin/zsh", "/bin/sh"}
	for _, shell := range shells {
		if _, err := os.Stat(shell); err == nil {
			return shell
		}
	}
	return "/bin/sh"
}
