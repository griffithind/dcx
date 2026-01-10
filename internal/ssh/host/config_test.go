package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			name: "single entry removal",
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
			name: "entry in middle",
			content: `Host example.com
  User git

# DCX managed - test-container
Host test-container.dcx
  ProxyCommand dcx ssh --stdio test-container
  User root
# End DCX - test-container

Host another.com
  User admin
`,
			containerName: "test-container",
			// Note: implementation preserves empty lines that were around the removed block
			want: `Host example.com
  User git


Host another.com
  User admin
`,
		},
		{
			name: "multiple dcx entries",
			content: `# DCX managed - container1
Host container1.dcx
  ProxyCommand dcx ssh --stdio container1
# End DCX - container1

# DCX managed - container2
Host container2.dcx
  ProxyCommand dcx ssh --stdio container2
# End DCX - container2
`,
			containerName: "container1",
			// Note: the empty line between entries is preserved as a leading newline
			want: `
# DCX managed - container2
Host container2.dcx
  ProxyCommand dcx ssh --stdio container2
# End DCX - container2
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
	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "ssh-config-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Override the home directory for testing
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// Create .ssh directory
	sshDir := filepath.Join(tmpDir, ".ssh")
	err = os.MkdirAll(sshDir, 0700)
	require.NoError(t, err)

	t.Run("create new config", func(t *testing.T) {
		err := AddSSHConfig("test.dcx", "test-container", "root")
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(sshDir, "config"))
		require.NoError(t, err)

		assert.Contains(t, string(content), "# DCX managed - test-container")
		assert.Contains(t, string(content), "Host test.dcx")
		assert.Contains(t, string(content), "User root")
		assert.Contains(t, string(content), "# End DCX - test-container")
	})

	t.Run("update existing config", func(t *testing.T) {
		// Update with different user
		err := AddSSHConfig("test.dcx", "test-container", "vscode")
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(sshDir, "config"))
		require.NoError(t, err)

		// Should only have one entry
		count := strings.Count(string(content), "# DCX managed - test-container")
		assert.Equal(t, 1, count)
		assert.Contains(t, string(content), "User vscode")
	})
}

func TestRemoveSSHConfig(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "ssh-config-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Override the home directory for testing
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// Create .ssh directory and config
	sshDir := filepath.Join(tmpDir, ".ssh")
	err = os.MkdirAll(sshDir, 0700)
	require.NoError(t, err)

	t.Run("remove non-existent config file", func(t *testing.T) {
		// Should not error
		err := RemoveSSHConfig("test-container")
		assert.NoError(t, err)
	})

	t.Run("remove existing entry", func(t *testing.T) {
		// Add an entry first
		err := AddSSHConfig("test.dcx", "test-container", "root")
		require.NoError(t, err)

		// Verify it exists
		content, _ := os.ReadFile(filepath.Join(sshDir, "config"))
		assert.Contains(t, string(content), "test-container")

		// Remove it
		err = RemoveSSHConfig("test-container")
		require.NoError(t, err)

		// Verify it's gone
		content, _ = os.ReadFile(filepath.Join(sshDir, "config"))
		assert.NotContains(t, string(content), "test-container")
	})
}

func TestHasSSHConfig(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "ssh-config-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Override the home directory for testing
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// Create .ssh directory
	sshDir := filepath.Join(tmpDir, ".ssh")
	err = os.MkdirAll(sshDir, 0700)
	require.NoError(t, err)

	t.Run("no config file", func(t *testing.T) {
		assert.False(t, HasSSHConfig("test-container"))
	})

	t.Run("config exists", func(t *testing.T) {
		err := AddSSHConfig("test.dcx", "test-container", "root")
		require.NoError(t, err)

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
