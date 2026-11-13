package hostconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func addSSHConfig(t *testing.T, e Entry) []string {
	t.Helper()
	aliases, err := AddSSHConfig(e)
	require.NoError(t, err)
	return aliases
}

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
		_, err := AddSSHConfig(sampleEntry())
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(sshDir, "dcx", "config"))
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
		_, err := AddSSHConfig(e)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(sshDir, "dcx", "config"))
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
	addSSHConfig(t, e)

	content, _ := os.ReadFile(filepath.Join(tmpDir, ".ssh", "dcx", "config"))
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
		addSSHConfig(t, sampleEntry())

		err := RemoveSSHConfig("test-container")
		require.NoError(t, err)

		for _, f := range []string{"config", "dcx/config"} {
			content, _ := os.ReadFile(filepath.Join(tmpDir, ".ssh", f))
			assert.NotContains(t, string(content), "test-container", f)
		}
	})
}

func TestAliasesFindsBlock(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".ssh"), 0700))

	t.Run("no config file", func(t *testing.T) {
		assert.False(t, Aliases("test-container") != nil)
	})

	t.Run("config exists", func(t *testing.T) {
		addSSHConfig(t, sampleEntry())

		assert.True(t, Aliases("test-container") != nil)
		assert.False(t, Aliases("other-container") != nil)
	})
}

func TestGetSSHConfigPath(t *testing.T) {
	path := getSSHConfigPath()
	assert.Contains(t, path, ".ssh")
	assert.Contains(t, path, "config")
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

	addSSHConfig(t, sampleEntry())

	main, _ := os.ReadFile(filepath.Join(tmpDir, ".ssh", "config"))
	assert.NotContains(t, string(main), "ProxyCommand", "legacy block should move out of ~/.ssh/config")
	assert.NotContains(t, string(main), "test-container")
	assert.True(t, strings.HasPrefix(string(main), "# Added by dcx"), "include goes at the top")

	content, _ := os.ReadFile(filepath.Join(tmpDir, ".ssh", "dcx", "config"))
	s := string(content)
	assert.Contains(t, s, "HostName 127.0.0.1")
	assert.Contains(t, s, "Port 53412")
}

// Removing "api" must not touch the block for "api-v2", whose start marker
// shares the prefix. Previously the prefix match swallowed everything after
// api-v2's start marker.
func TestRemoveSSHConfigEntryDoesNotMatchPrefix(t *testing.T) {
	content := "Host personal\n  User me\n\n" +
		"# DCX managed - api-v2\nHost v2.dcx\n  Port 1\n# End DCX - api-v2\n\n" +
		"Host after\n  User keep\n"

	got := string(removeSSHConfigEntry([]byte(content), "api"))
	assert.Equal(t, strings.TrimRight(content, "\n")+"\n", got)
}

func TestRemoveSSHConfigEntryRemovesOnlyExactBlock(t *testing.T) {
	content := "# DCX managed - api\nHost api.dcx\n# End DCX - api\n\n" +
		"# DCX managed - api-v2\nHost v2.dcx\n# End DCX - api-v2\n\n" +
		"Host after\n"

	got := string(removeSSHConfigEntry([]byte(content), "api"))
	assert.NotContains(t, got, "api.dcx")
	assert.Contains(t, got, "# DCX managed - api-v2\nHost v2.dcx\n# End DCX - api-v2")
	assert.Contains(t, got, "Host after")
}

// A start marker whose end marker is missing (hand-edited or truncated file)
// must not cause the rest of the user's config to be deleted.
func TestRemoveSSHConfigEntryKeepsUnterminatedBlock(t *testing.T) {
	content := "# DCX managed - api\nHost api.dcx\nHost precious\n  User me\n"
	got := string(removeSSHConfigEntry([]byte(content), "api"))
	assert.Contains(t, got, "Host precious")
}

func TestAliasesExactMatch(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".ssh"), 0700))

	entry := sampleEntry()
	entry.ContainerName = "api-v2"
	addSSHConfig(t, entry)

	assert.True(t, Aliases("api-v2") != nil)
	assert.False(t, Aliases("api") != nil)
}

// Writes go through a temp file + rename; a symlinked ~/.ssh/config (dotfile
// managers) must stay a symlink and the target's mode must be preserved.
func TestAddSSHConfigPreservesSymlinkAndMode(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	sshDir := filepath.Join(tmpDir, ".ssh")
	require.NoError(t, os.MkdirAll(sshDir, 0700))

	real := filepath.Join(tmpDir, "dotfiles-ssh-config")
	require.NoError(t, os.WriteFile(real, []byte("Host mine\n  User me\n"), 0640))
	require.NoError(t, os.Symlink(real, filepath.Join(sshDir, "config")))

	addSSHConfig(t, sampleEntry())

	info, err := os.Lstat(filepath.Join(sshDir, "config"))
	require.NoError(t, err)
	assert.True(t, info.Mode()&os.ModeSymlink != 0, "~/.ssh/config should still be a symlink")

	realInfo, err := os.Stat(real)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0640), realInfo.Mode().Perm())

	data, err := os.ReadFile(real)
	require.NoError(t, err)
	assert.Contains(t, string(data), "Host mine")
	assert.Contains(t, string(data), includeLine)
}

// An unreadable config must produce an error, not be overwritten with only
// the dcx block.
func TestAddSSHConfigFailsOnUnreadableConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read any file")
	}
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	sshDir := filepath.Join(tmpDir, ".ssh")
	require.NoError(t, os.MkdirAll(sshDir, 0700))
	cfg := filepath.Join(sshDir, "config")
	require.NoError(t, os.WriteFile(cfg, []byte("Host mine\n"), 0000))

	_, err := AddSSHConfig(sampleEntry())
	assert.Error(t, err)

	require.NoError(t, os.Chmod(cfg, 0600))
	data, err := os.ReadFile(cfg)
	require.NoError(t, err)
	assert.Equal(t, "Host mine\n", string(data))
}

// The include must come first: OpenSSH takes the first value of each
// option, so a user `Host *` block above it would override dcx settings.
func TestAddSSHConfigIncludesAtTopOnce(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	sshDir := filepath.Join(tmpDir, ".ssh")
	require.NoError(t, os.MkdirAll(sshDir, 0700))
	user := "Host *\n  User someone-else\n  StrictHostKeyChecking ask\n"
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "config"), []byte(user), 0600))

	addSSHConfig(t, sampleEntry())
	e := sampleEntry()
	e.ContainerName, e.HostName = "other-1234", "other.dcx"
	addSSHConfig(t, e)

	main, err := os.ReadFile(filepath.Join(sshDir, "config"))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(main), includeLine))
	assert.Less(t, strings.Index(string(main), includeLine), strings.Index(string(main), "Host *"))
	assert.True(t, strings.HasSuffix(string(main), user), "the user's config is otherwise unchanged")
}

func TestAddSSHConfigLeavesUserConfigUntouchedWhenIncluded(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	sshDir := filepath.Join(tmpDir, ".ssh")
	require.NoError(t, os.MkdirAll(sshDir, 0700))
	user := includeLine + "\n\nHost mine\n  User me\n\n\n"
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "config"), []byte(user), 0600))

	addSSHConfig(t, sampleEntry())
	require.NoError(t, RemoveSSHConfig("test-container"))

	main, err := os.ReadFile(filepath.Join(sshDir, "config"))
	require.NoError(t, err)
	assert.Equal(t, user, string(main))
}

// Two checkouts with the same name: the first keeps "<name>.dcx", the
// second is reachable by its unique "<container>.dcx" only.
func TestAddSSHConfigAliasCollision(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	first := sampleEntry()
	first.ContainerName = "test-aaaa1111"
	assert.Equal(t, []string{"test.dcx", "test-aaaa1111.dcx"}, addSSHConfig(t, first))

	second := sampleEntry()
	second.ContainerName = "test-bbbb2222"
	assert.Equal(t, []string{"test-bbbb2222.dcx"}, addSSHConfig(t, second))

	// Rewriting the first keeps its short alias.
	assert.Equal(t, []string{"test.dcx", "test-aaaa1111.dcx"}, addSSHConfig(t, first))
	assert.Equal(t, []string{"test.dcx", "test-aaaa1111.dcx"}, Aliases("test-aaaa1111"))
	assert.Equal(t, []string{"test-bbbb2222.dcx"}, Aliases("test-bbbb2222"))

	// Once the first is gone, the second can take the short alias.
	require.NoError(t, RemoveSSHConfig("test-aaaa1111"))
	assert.Equal(t, []string{"test.dcx", "test-bbbb2222.dcx"}, addSSHConfig(t, second))
}

func TestSSHDirOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(t.TempDir(), "isolated-ssh")
	t.Setenv(SSHDirEnv, dir)

	addSSHConfig(t, sampleEntry())

	main, err := os.ReadFile(filepath.Join(dir, "config"))
	require.NoError(t, err)
	assert.Contains(t, string(main), "Include "+filepath.Join(dir, "dcx", "config"), "the include points at the relocated file")
	_, err = os.ReadFile(filepath.Join(dir, "dcx", "config"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(home, ".ssh"))
	assert.True(t, os.IsNotExist(err), "~/.ssh must not be touched")
	assert.Equal(t, filepath.Join(dir, "config"), ConfigFileOverride())
}
