package env

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/client"
	"github.com/griffithind/dcx/internal/container"
)

const (
	// MarkerDir is the directory where patch marker files are stored.
	MarkerDir = "/var/lib/dcx"

	// MarkerEtcEnvironment indicates /etc/environment has been patched.
	MarkerEtcEnvironment = MarkerDir + "/.patchEtcEnvironmentMarker"

	// MarkerEtcProfile indicates /etc/profile has been patched.
	MarkerEtcProfile = MarkerDir + "/.patchEtcProfileMarker"
)

// Patcher handles patching container system files for proper environment setup.
type Patcher struct {
	dockerClient *client.Client
}

// NewPatcher creates a new Patcher.
func NewPatcher(dockerClient *client.Client) *Patcher {
	return &Patcher{
		dockerClient: dockerClient,
	}
}

// PatchEtcEnvironment appends environment variables to /etc/environment.
// This makes them available to all processes, not just shells.
// Uses a marker file to ensure it only runs once per container.
func (p *Patcher) PatchEtcEnvironment(ctx context.Context, containerID string, env map[string]string) error {
	if len(env) == 0 {
		return nil
	}

	// Check if already patched
	checkCmd := []string{"sh", "-c", fmt.Sprintf("test -f %s && echo exists || echo missing", MarkerEtcEnvironment)}
	output, exitCode, err := container.ExecOutput(ctx, p.dockerClient, containerID, checkCmd, "root")
	if err != nil {
		return fmt.Errorf("failed to check patch marker: %w", err)
	}
	if exitCode == 0 && strings.TrimSpace(output) == "exists" {
		// Already patched
		return nil
	}

	// Build environment content
	var envLines []string
	for k, v := range env {
		// Escape special characters in value
		escaped := strings.ReplaceAll(v, `"`, `\"`)
		envLines = append(envLines, fmt.Sprintf(`%s="%s"`, k, escaped))
	}
	envContent := strings.Join(envLines, "\n")

	// Create marker directory and append to /etc/environment
	patchCmd := []string{"sh", "-c", fmt.Sprintf(`
mkdir -p %s && \
cat >> /etc/environment <<'DCXEOF'
%s
DCXEOF
touch %s
`, MarkerDir, envContent, MarkerEtcEnvironment)}

	_, exitCode, err = container.ExecOutput(ctx, p.dockerClient, containerID, patchCmd, "root")
	if err != nil {
		return fmt.Errorf("failed to patch /etc/environment: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("patch /etc/environment exited with code %d", exitCode)
	}

	return nil
}

// PatchEtcProfile modifies /etc/profile to preserve existing PATH.
// This prevents the system profile from clobbering PATH modifications made by features.
// Uses a marker file to ensure it only runs once per container.
func (p *Patcher) PatchEtcProfile(ctx context.Context, containerID string) error {
	// Check if already patched
	checkCmd := []string{"sh", "-c", fmt.Sprintf("test -f %s && echo exists || echo missing", MarkerEtcProfile)}
	output, exitCode, err := container.ExecOutput(ctx, p.dockerClient, containerID, checkCmd, "root")
	if err != nil {
		return fmt.Errorf("failed to check patch marker: %w", err)
	}
	if exitCode == 0 && strings.TrimSpace(output) == "exists" {
		// Already patched
		return nil
	}

	// Patch /etc/profile to preserve PATH
	// The regex transforms: PATH=/some/path -> PATH=${PATH:-/some/path}
	// This makes PATH respect any pre-existing value, or fall back to the original
	patchCmd := []string{"sh", "-c", fmt.Sprintf(`
mkdir -p %s && \
sed -i -E 's/((^|[[:space:]])PATH=)([^$]*)$/\1\${PATH:-\3}/g' /etc/profile 2>/dev/null || true && \
touch %s
`, MarkerDir, MarkerEtcProfile)}

	_, exitCode, err = container.ExecOutput(ctx, p.dockerClient, containerID, patchCmd, "root")
	if err != nil {
		return fmt.Errorf("failed to patch /etc/profile: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("patch /etc/profile exited with code %d", exitCode)
	}

	return nil
}
