package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/output"
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

// CheckResult represents a single check result.
type CheckResult struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// DoctorOutput represents the doctor output for JSON.
type DoctorOutput struct {
	Checks  []CheckResult `json:"checks"`
	AllOK   bool          `json:"allOk"`
}

func runDoctor(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	out := output.Global()
	c := out.Color()

	var results []CheckResult

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

	// Calculate overall status
	allOK := true
	for _, r := range results {
		if !r.OK {
			allOK = false
			break
		}
	}

	// JSON output mode
	if out.IsJSON() {
		return out.JSON(DoctorOutput{
			Checks: results,
			AllOK:  allOK,
		})
	}

	// Text output mode
	out.Println(c.Header("dcx doctor"))
	out.Println(c.Dim("=========="))
	out.Println()

	for _, r := range results {
		var status string
		if r.OK {
			status = output.FormatCheck(output.CheckResultPass, fmt.Sprintf("%s: %s", r.Name, r.Message))
		} else {
			status = output.FormatCheck(output.CheckResultFail, fmt.Sprintf("%s: %s", r.Name, r.Message))
		}
		out.Println(status)
	}

	out.Println()
	if allOK {
		out.Println(output.FormatSuccess("All checks passed!"))
		return nil
	}

	return fmt.Errorf("some checks failed")
}

func checkDocker(ctx context.Context) CheckResult {
	client, err := docker.NewClient()
	if err != nil {
		return CheckResult{
			Name:    "Docker",
			OK:      false,
			Message: fmt.Sprintf("failed to connect: %v", err),
		}
	}
	defer client.Close()

	if err := client.Ping(ctx); err != nil {
		return CheckResult{
			Name:    "Docker",
			OK:      false,
			Message: fmt.Sprintf("daemon not responding: %v", err),
		}
	}

	version, err := client.ServerVersion(ctx)
	if err != nil {
		return CheckResult{
			Name:    "Docker",
			OK:      true,
			Message: "connected (version unknown)",
		}
	}

	return CheckResult{
		Name:    "Docker",
		OK:      true,
		Message: fmt.Sprintf("version %s", version),
	}
}

func checkCompose() CheckResult {
	// Check for docker compose (v2 plugin)
	cmd := exec.Command("docker", "compose", "version", "--short")
	cmdOutput, err := cmd.Output()
	if err == nil {
		return CheckResult{
			Name:    "Docker Compose",
			OK:      true,
			Message: fmt.Sprintf("version %s", string(cmdOutput[:len(cmdOutput)-1])),
		}
	}

	// Check for docker-compose (standalone)
	cmd = exec.Command("docker-compose", "version", "--short")
	cmdOutput, err = cmd.Output()
	if err == nil {
		return CheckResult{
			Name:    "Docker Compose",
			OK:      true,
			Message: fmt.Sprintf("version %s (standalone)", string(cmdOutput[:len(cmdOutput)-1])),
		}
	}

	return CheckResult{
		Name:    "Docker Compose",
		OK:      false,
		Message: "not found (install docker compose plugin or docker-compose)",
	}
}

func checkSSHAgent() CheckResult {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return CheckResult{
			Name:    "SSH Agent",
			OK:      false,
			Message: "SSH_AUTH_SOCK not set",
		}
	}

	if _, err := os.Stat(sock); err != nil {
		return CheckResult{
			Name:    "SSH Agent",
			OK:      false,
			Message: fmt.Sprintf("socket not accessible: %v", err),
		}
	}

	// Validate it's actually a socket
	if err := ssh.ValidateSocket(sock); err != nil {
		return CheckResult{
			Name:    "SSH Agent",
			OK:      false,
			Message: fmt.Sprintf("invalid socket: %v", err),
		}
	}

	return CheckResult{
		Name:    "SSH Agent",
		OK:      true,
		Message: fmt.Sprintf("available at %s", sock),
	}
}

func checkSELinux() CheckResult {
	mode, err := selinux.GetMode()
	if err != nil {
		return CheckResult{
			Name:    "SELinux",
			OK:      true,
			Message: "not available (expected on non-SELinux systems)",
		}
	}

	switch mode {
	case selinux.ModeEnforcing:
		return CheckResult{
			Name:    "SELinux",
			OK:      true,
			Message: "enforcing (will use :Z bind mount option)",
		}
	case selinux.ModePermissive:
		return CheckResult{
			Name:    "SELinux",
			OK:      true,
			Message: "permissive",
		}
	case selinux.ModeDisabled:
		return CheckResult{
			Name:    "SELinux",
			OK:      true,
			Message: "disabled",
		}
	default:
		return CheckResult{
			Name:    "SELinux",
			OK:      true,
			Message: fmt.Sprintf("mode: %s", mode),
		}
	}
}
