package hostconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"os"

	"github.com/griffithind/dcx/internal/dcxhome"
)

// maxControlPath is the longest control socket path OpenSSH can use: a
// socket path holds at most 104 bytes (macOS; 108 on Linux), and OpenSSH
// appends a 17-character temporary suffix while creating it. A longer
// path fails the whole connection, not just the sharing.
const maxControlPath = 104 - 1 - 17

// controlPersist is how long a shared connection stays open after its
// last session ends.
const controlPersist = "10m"

// ShareOptions returns the ssh options dcx adds to its own connections to
// a workspace (`dcx ssh`, lifecycle hooks), so they share one connection
// (OpenSSH's ControlMaster) instead of setting one up each time (about
// 100 ms; a session on a shared one takes a few). Nil when the control
// socket path would be too long, so connections are made one by one.
//
// A shared connection forwards the SSH agent of the shell that opened it,
// whatever the agent of later sessions. So the socket is per agent socket
// (SSH_AUTH_SOCK): a shell with another agent (a new login with -A, a tmux
// pane with a stale variable) gets its own connection. An agent restarted
// at the same path keeps working: OpenSSH opens the path again for every
// request. Plain `ssh <alias>` is left unshared, since its config cannot
// tell agents apart.
func ShareOptions(workspaceID string) []string {
	path, ok := controlPath(workspaceID, os.Getenv("SSH_AUTH_SOCK"))
	if !ok {
		return nil
	}
	return []string{"-o", "ControlMaster=auto", "-o", "ControlPath=" + path, "-o", "ControlPersist=" + controlPersist}
}

// controlPath returns the workspace's control socket for agentSock:
// $DCX_HOME/workspaces/<id>/ssh-<hash of agentSock>, its folder created
// private. ok is false when it cannot be used.
func controlPath(workspaceID, agentSock string) (path string, ok bool) {
	sum := sha256.Sum256([]byte(agentSock))
	path, err := dcxhome.WorkspacePath(workspaceID, dcxhome.ControlSocketPrefix+hex.EncodeToString(sum[:4]))
	if err != nil || len(path) > maxControlPath {
		return "", false
	}
	dir, err := dcxhome.WorkspaceDir(workspaceID)
	if err != nil || os.MkdirAll(dir, 0o700) != nil {
		return "", false
	}
	return path, true
}
