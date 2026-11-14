package helpers

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/griffithind/dcx/internal/dcxhome"
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
	stdout, stderr, err := RunDCXInDir(t, dir, "status", "--json")
	require.NoError(t, err, "dcx status --json: %s", stderr)
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
// Isolate); "" before it is set up.
func SSHDir() string { return os.Getenv(hostconfig.SSHDirEnv) }

// Isolate points dcx at temporary directories instead of ~/.ssh (SSH
// config) and ~/.dcx (host keys, known_hosts, the fallback client key,
// locks, compose overrides), so the tests never touch the developer's, and
// replaces the SSH agent with one holding a generated key (see
// AgentKeyFingerprint). The returned function undoes it; call Isolate from
// TestMain.
func Isolate() (cleanup func(), err error) {
	sshDir, err := os.MkdirTemp("", "dcx-e2e-ssh-")
	if err != nil {
		return nil, err
	}
	home, err := os.MkdirTemp("", "dcx-e2e-home-")
	if err != nil {
		_ = os.RemoveAll(sshDir)
		return nil, err
	}
	cleanup = func() {
		_ = os.RemoveAll(sshDir)
		_ = os.RemoveAll(home)
	}
	if err := errors.Join(os.Setenv(hostconfig.SSHDirEnv, sshDir), os.Setenv(dcxhome.Env, home)); err != nil {
		cleanup()
		return nil, err
	}
	agentListener, err := startSSHAgent(home)
	if err != nil {
		cleanup()
		return nil, err
	}
	removeDirs := cleanup
	cleanup = func() {
		_ = agentListener.Close()
		removeDirs()
	}
	return cleanup, nil
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

// ContainerID returns the ID of the workspace's primary container.
func ContainerID(t *testing.T, dir string) string {
	t.Helper()
	s := GetStatus(t, dir)
	require.NotNil(t, s.Container, "workspace %s has no container (state %s)", dir, s.State)
	return s.Container.ID
}

// ContainerImageID returns the ID of the image the workspace's primary
// container runs: unchanged when an up reused the cached image layers.
func ContainerImageID(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("docker", "inspect", "--format", "{{.Image}}", ContainerID(t, dir)).Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}
