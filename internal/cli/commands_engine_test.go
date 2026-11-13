package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withFakeEngine runs commands against fake (the engine factories in
// context.go) in a workspace holding config, restoring the globals after.
func withFakeEngine(t *testing.T, fake *containertest.Fake, config string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	ws := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".devcontainer"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, ".devcontainer", "devcontainer.json"), []byte(config), 0o644))

	oldEngine, oldDaemon, oldWS, oldCfg := newEngine, newDaemonEngine, workspacePath, configPath
	newEngine = func() container.Engine { return fake }
	newDaemonEngine = func() (container.Engine, error) { return fake, nil }
	workspacePath, configPath = ws, ""
	t.Cleanup(func() {
		newEngine, newDaemonEngine, workspacePath, configPath = oldEngine, oldDaemon, oldWS, oldCfg
		doctorConfig, doctorSystem = false, false
		cleanAll, cleanDangling, cleanCurrent, dryRun = false, false, false, false
	})
}

func TestDoctorReportsSystemAndConfig(t *testing.T) {
	fake := &containertest.Fake{SELinux: true}
	withFakeEngine(t, fake, `{"image": "alpine:3"}`)
	out := captureUI(t, func() { require.NoError(t, runDoctor(doctorCmd, nil)) })
	assert.Contains(t, out, "Docker: version 28.0.0")
	assert.Contains(t, out, "Docker Compose: version 2.40.0")
	assert.Contains(t, out, "Docker labels containers; dcx relabels the workspace mount")
	assert.Contains(t, out, "image plan, base alpine:3")
	assert.Contains(t, out, "All checks passed!")
}

func TestDoctorFailsOnABrokenConfig(t *testing.T) {
	withFakeEngine(t, &containertest.Fake{}, `{"image": "alpine:3", "dockerComposeFile": "c.yml", "service": "app"}`)
	doctorConfig = true
	var err error
	out := captureUI(t, func() { err = runDoctor(doctorCmd, nil) })
	require.Error(t, err)
	assert.Contains(t, out, "set only one of image, build or dockerComposeFile")
	assert.NotContains(t, out, "System Requirements", "--config-only skips the system checks")
}

func TestCleanGoesThroughTheService(t *testing.T) {
	fake := &containertest.Fake{}
	withFakeEngine(t, fake, `{"image": "alpine:3"}`)
	cleanCurrent = true
	captureUI(t, func() { require.NoError(t, runClean(cleanCmd, nil)) })
	calls := fake.CallsTo("CleanupDerivedImages")
	require.Len(t, calls, 1)
	assert.NotEmpty(t, calls[0].Args[0], "--current scopes the cleanup to this workspace")
	assert.Empty(t, fake.CallsTo("CleanupDanglingImages"))
}
