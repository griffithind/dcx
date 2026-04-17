package hostconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleEntry() Entry {
	return Entry{
		HostName:      "test.dcx",
		ContainerName: "test-container",
		WorkspaceID:   "wk_test",
		User:          "root",
		BindHost:      "127.0.0.1",
		Port:          53412,
	}
}

func TestRemoveSSHConfigEntry(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		containerName string
		want          string
	}{
		{
			name:          "empty content",
			content:       "",
			containerName: "test-container",
			want:          "",
		},
		{
			name:          "no matching entry",
			content:       "Host example.com\n  User git\n",
			containerName: "test-container",
			want:          "Host example.com\n  User git\n",
		},
		{
			name: "single entry removal (legacy ProxyCommand form)",
			content: `# DCX managed - test-container
Host test-container.dcx
  ProxyCommand dcx ssh --stdio test-container
  User root
# End DCX - test-container
`,
			containerName: "test-container",
			want:          "",
		},
		{
			name: "single entry removal (TCP form)",
			content: `# DCX managed - test-container
Host test-container.dcx
  HostName 127.0.0.1
  Port 53412
  User root
# End DCX - test-container
`,
			containerName: "test-container",
			want:          "",
		},
		{
			name: "entry in middle",
			content: `Host example.com
  User git

# DCX managed - test-container
Host test-container.dcx
  HostName 127.0.0.1
  Port 53412
# End DCX - test-container

Host another.com
  User admin
`,
			containerName: "test-container",
			want: `Host example.com
  User git


Host another.com
  User admin
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeSSHConfigEntry([]byte(tt.content), tt.containerName)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func TestAddSSHConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	sshDir := filepath.Join(tmpDir, ".ssh")
	err := os.MkdirAll(sshDir, 0700)
	require.NoError(t, err)

	t.Run("create new config", func(t *testing.T) {
		err := AddSSHConfig(sampleEntry())
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(sshDir, "config"))
		require.NoError(t, err)
		s := string(content)

		assert.Contains(t, s, "# DCX managed - test-container")
		assert.Contains(t, s, "Host test.dcx")
		assert.Contains(t, s, "HostName 127.0.0.1")
		assert.Contains(t, s, "Port 53412")
		assert.Contains(t, s, "User root")
		assert.Contains(t, s, "HostKeyAlias dcx-wk_test")
		assert.Contains(t, s, "# End DCX - test-container")

		// Crucially — no ProxyCommand any more.
		assert.NotContains(t, s, "ProxyCommand")
	})

	t.Run("update existing config", func(t *testing.T) {
		e := sampleEntry()
		e.User = "vscode"
		err := AddSSHConfig(e)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(sshDir, "config"))
		require.NoError(t, err)

		count := strings.Count(string(content), "# DCX managed - test-container")
		assert.Equal(t, 1, count)
		assert.Contains(t, string(content), "User vscode")
	})
}

func TestAddSSHConfigStrictHostKeyCheckingWhenKnownHostsSet(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".ssh"), 0700))

	e := sampleEntry()
	e.KnownHostsPath = filepath.Join(tmpDir, ".dcx", "known_hosts")
	require.NoError(t, AddSSHConfig(e))

	content, _ := os.ReadFile(filepath.Join(tmpDir, ".ssh", "config"))
	assert.Contains(t, string(content), "StrictHostKeyChecking yes")
	assert.Contains(t, string(content), "UserKnownHostsFile "+e.KnownHostsPath)
}

func TestRemoveSSHConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".ssh"), 0700))

	t.Run("remove non-existent config file", func(t *testing.T) {
		err := RemoveSSHConfig("test-container")
		assert.NoError(t, err)
	})

	t.Run("remove existing entry", func(t *testing.T) {
		require.NoError(t, AddSSHConfig(sampleEntry()))

		err := RemoveSSHConfig("test-container")
		require.NoError(t, err)

		content, _ := os.ReadFile(filepath.Join(tmpDir, ".ssh", "config"))
		assert.NotContains(t, string(content), "test-container")
	})
}

func TestHasSSHConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".ssh"), 0700))

	t.Run("no config file", func(t *testing.T) {
		assert.False(t, HasSSHConfig("test-container"))
	})

	t.Run("config exists", func(t *testing.T) {
		require.NoError(t, AddSSHConfig(sampleEntry()))

		assert.True(t, HasSSHConfig("test-container"))
		assert.False(t, HasSSHConfig("other-container"))
	})
}

func TestGetSSHConfigPath(t *testing.T) {
	path := getSSHConfigPath()
	assert.Contains(t, path, ".ssh")
	assert.Contains(t, path, "config")
}

func TestSSHConfigConstants(t *testing.T) {
	assert.Contains(t, sshConfigMarkerStart, "DCX")
	assert.Contains(t, sshConfigMarkerEnd, "DCX")
}

// TestUpgradeFromLegacyStdioBlock verifies that an existing stdio-era
// (ProxyCommand) config block is cleanly replaced by the new TCP block on
// next AddSSHConfig, so no user action is needed on upgrade.
func TestUpgradeFromLegacyStdioBlock(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".ssh"), 0700))

	legacy := `# DCX managed - test-container
Host test.dcx
  ProxyCommand dcx ssh --stdio test-container
  User root
  ForwardAgent yes
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null
  LogLevel ERROR
# End DCX - test-container
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".ssh", "config"), []byte(legacy), 0600))

	require.NoError(t, AddSSHConfig(sampleEntry()))

	content, _ := os.ReadFile(filepath.Join(tmpDir, ".ssh", "config"))
	s := string(content)
	assert.NotContains(t, s, "ProxyCommand", "legacy ProxyCommand should be removed")
	assert.Contains(t, s, "HostName 127.0.0.1")
	assert.Contains(t, s, "Port 53412")
}
