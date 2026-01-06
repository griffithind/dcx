package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/selinux"
	"github.com/griffithind/dcx/internal/ssh"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system requirements",
	Long: `Check that all system requirements are met for running dcx.

This command checks:
- Docker daemon connectivity
- Docker Compose availability
- SSH agent availability
- SELinux status (Linux only)`,
	RunE: runDoctor,
}

type checkResult struct {
	name    string
	ok      bool
	message string
}

func runDoctor(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	var results []checkResult

	// Check Docker daemon
	results = append(results, checkDocker(ctx))

	// Check Docker Compose
	results = append(results, checkCompose())

	// Check SSH agent
	results = append(results, checkSSHAgent())

	// Check SELinux (Linux only)
	if runtime.GOOS == "linux" {
		results = append(results, checkSELinux())
	}

	// Print results
	fmt.Println("dcx doctor")
	fmt.Println("==========")
	fmt.Println()

	allOK := true
	for _, r := range results {
		status := "✓"
		if !r.ok {
			status = "✗"
			allOK = false
		}
		fmt.Printf("[%s] %s: %s\n", status, r.name, r.message)
	}

	fmt.Println()
	if allOK {
		fmt.Println("All checks passed!")
		return nil
	}

	return fmt.Errorf("some checks failed")
}

func checkDocker(ctx context.Context) checkResult {
	client, err := docker.NewClient()
	if err != nil {
		return checkResult{
			name:    "Docker",
			ok:      false,
			message: fmt.Sprintf("failed to connect: %v", err),
		}
	}
	defer client.Close()

	if err := client.Ping(ctx); err != nil {
		return checkResult{
			name:    "Docker",
			ok:      false,
			message: fmt.Sprintf("daemon not responding: %v", err),
		}
	}

	version, err := client.ServerVersion(ctx)
	if err != nil {
		return checkResult{
			name:    "Docker",
			ok:      true,
			message: "connected (version unknown)",
		}
	}

	return checkResult{
		name:    "Docker",
		ok:      true,
		message: fmt.Sprintf("version %s", version),
	}
}

func checkCompose() checkResult {
	// Check for docker compose (v2 plugin)
	cmd := exec.Command("docker", "compose", "version", "--short")
	output, err := cmd.Output()
	if err == nil {
		return checkResult{
			name:    "Docker Compose",
			ok:      true,
			message: fmt.Sprintf("version %s", string(output[:len(output)-1])),
		}
	}

	// Check for docker-compose (standalone)
	cmd = exec.Command("docker-compose", "version", "--short")
	output, err = cmd.Output()
	if err == nil {
		return checkResult{
			name:    "Docker Compose",
			ok:      true,
			message: fmt.Sprintf("version %s (standalone)", string(output[:len(output)-1])),
		}
	}

	return checkResult{
		name:    "Docker Compose",
		ok:      false,
		message: "not found (install docker compose plugin or docker-compose)",
	}
}

func checkSSHAgent() checkResult {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return checkResult{
			name:    "SSH Agent",
			ok:      false,
			message: "SSH_AUTH_SOCK not set",
		}
	}

	if _, err := os.Stat(sock); err != nil {
		return checkResult{
			name:    "SSH Agent",
			ok:      false,
			message: fmt.Sprintf("socket not accessible: %v", err),
		}
	}

	// Validate it's actually a socket
	if err := ssh.ValidateSocket(sock); err != nil {
		return checkResult{
			name:    "SSH Agent",
			ok:      false,
			message: fmt.Sprintf("invalid socket: %v", err),
		}
	}

	return checkResult{
		name:    "SSH Agent",
		ok:      true,
		message: fmt.Sprintf("available at %s", sock),
	}
}

func checkSELinux() checkResult {
	mode, err := selinux.GetMode()
	if err != nil {
		return checkResult{
			name:    "SELinux",
			ok:      true,
			message: "not available (expected on non-SELinux systems)",
		}
	}

	switch mode {
	case selinux.ModeEnforcing:
		return checkResult{
			name:    "SELinux",
			ok:      true,
			message: "enforcing (will use :Z bind mount option)",
		}
	case selinux.ModePermissive:
		return checkResult{
			name:    "SELinux",
			ok:      true,
			message: "permissive",
		}
	case selinux.ModeDisabled:
		return checkResult{
			name:    "SELinux",
			ok:      true,
			message: "disabled",
		}
	default:
		return checkResult{
			name:    "SELinux",
			ok:      true,
			message: fmt.Sprintf("mode: %s", mode),
		}
	}
}
