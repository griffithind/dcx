package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Sessions use the container's SHELL, else the user's login shell, else
// /bin/sh (M4/M14), as devcontainers/cli does.
func TestSessionShell(t *testing.T) {
	dir := t.TempDir()
	zsh := filepath.Join(dir, "zsh")
	fish := filepath.Join(dir, "fish")
	for _, p := range []string{zsh, fish} {
		require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755))
	}
	passwd := filepath.Join(dir, "passwd")
	require.NoError(t, os.WriteFile(passwd, []byte("root:x:0:0:root:/root:/bin/sh\nnode:x:1000:1000::/home/node:"+zsh+"\n"), 0o644))

	assert.Equal(t, fish, sessionShell(fish, "node", passwd), "the container's SHELL first")
	assert.Equal(t, zsh, sessionShell("", "node", passwd), "then the login shell")
	assert.Equal(t, zsh, sessionShell("", "1000", passwd), "a uid finds the same entry")
	assert.Equal(t, zsh, sessionShell(filepath.Join(dir, "missing"), "node", passwd), "a missing SHELL is skipped")
	assert.Equal(t, "/bin/sh", sessionShell("", "nobody", passwd))

	nologin := filepath.Join(dir, "nologin")
	require.NoError(t, os.WriteFile(nologin, []byte("#!/bin/sh\nexit 1\n"), 0o755))
	require.NoError(t, os.WriteFile(passwd, []byte("nobody:x:65534:65534:nobody:/:"+nologin+"\n"), 0o644))
	assert.Equal(t, "/bin/sh", sessionShell("", "nobody", passwd), "a nologin shell refuses sessions")
}
