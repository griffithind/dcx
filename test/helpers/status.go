package helpers

import (
	"encoding/json"
	"os"
	"os/exec"
	"sync"
	"testing"

	"github.com/griffithind/dcx/internal/ssh/hostconfig"
	"github.com/stretchr/testify/require"
)

// Status is the `dcx status --json` output (a stable contract, unlike the
// human output).
type Status struct {
	WorkspacePath string `json:"workspacePath"`
	WorkspaceID   string `json:"workspaceID"`
	Name          string `json:"name"`
	ResourceName  string `json:"resourceName"`
	State         string `json:"state"`
	SSHAlias      string `json:"sshAlias"`
	ConfigHash    string `json:"configHash"`
	Shortcuts     int    `json:"shortcuts"`
	Container     *struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		Running    bool   `json:"running"`
		ConfigHash string `json:"configHash"`
	} `json:"container"`
}

// GetStatus runs `dcx status --json` in dir.
func GetStatus(t *testing.T, dir string) Status {
	t.Helper()
	stdout := RunDCXInDirSuccess(t, dir, "status", "--json")
	var s Status
	require.NoError(t, json.Unmarshal([]byte(stdout), &s), "status --json output: %s", stdout)
	return s
}

// GetContainerState returns the workspace's state ("absent", "running", ...).
func GetContainerState(t *testing.T, dir string) string {
	t.Helper()
	return GetStatus(t, dir).State
}

// ContainerName returns the name of the workspace's primary container,
// failing the test when there is none.
func ContainerName(t *testing.T, dir string) string {
	t.Helper()
	s := GetStatus(t, dir)
	require.NotNil(t, s.Container, "workspace %s has no container (state %s)", dir, s.State)
	return s.Container.Name
}

var (
	dockerOnce sync.Once
	dockerErr  error
)

// RequireDockerAvailable skips the test when Docker is unreachable, or
// fails it when the CI environment variable is set: a CI run without a
// working daemon must not pass by skipping every test.
func RequireDockerAvailable(t *testing.T) {
	t.Helper()
	dockerOnce.Do(func() { dockerErr = exec.Command("docker", "info").Run() })
	if dockerErr == nil {
		return
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("Docker is not available in CI: %v", dockerErr)
	}
	t.Skip("Docker is not available, skipping E2E test")
}

// SSHDir is the directory the e2e run uses instead of ~/.ssh (see
// UseIsolatedSSHDir); "" before it is set up.
func SSHDir() string { return os.Getenv(hostconfig.SSHDirEnv) }

// UseIsolatedSSHDir points dcx (and SSHCommand) at a temporary directory
// instead of ~/.ssh, so the tests never modify the developer's SSH config.
// Call it from TestMain.
func UseIsolatedSSHDir() (string, error) {
	dir, err := os.MkdirTemp("", "dcx-e2e-ssh-")
	if err != nil {
		return "", err
	}
	return dir, os.Setenv(hostconfig.SSHDirEnv, dir)
}

// SSHCommand builds an OpenSSH command that uses the e2e run's SSH config.
func SSHCommand(args ...string) *exec.Cmd {
	if config := hostconfig.ConfigFileOverride(); config != "" {
		args = append([]string{"-F", config}, args...)
	}
	return exec.Command("ssh", args...)
}

// SFTPCommand builds an sftp command that uses the e2e run's SSH config.
func SFTPCommand(args ...string) *exec.Cmd {
	if config := hostconfig.ConfigFileOverride(); config != "" {
		args = append([]string{"-F", config}, args...)
	}
	return exec.Command("sftp", args...)
}
