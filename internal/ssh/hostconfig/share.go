package hostconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/griffithind/dcx/internal/dcxhome"
)

// Shared connections. dcx's own connections to a workspace (`dcx ssh`,
// lifecycle hooks) share one connection (OpenSSH's ControlMaster) instead
// of setting one up each time (about 100 ms; a session on a shared one
// takes a few).
//
// A shared connection forwards the SSH agent of the shell that opened it,
// whatever the agent of later sessions. So the socket is per agent socket
// (SSH_AUTH_SOCK): a shell with another agent (a new login with -A, a tmux
// pane with a stale variable) gets its own connection. An agent restarted
// at the same path keeps working: OpenSSH opens the path again for every
// request.
//
// The settings live in the workspace's config block, under a Match on
// ShareEnvVar, which only dcx sets (ShareEnv): the ssh command line stays
// `ssh <alias>` (terminals show it as the title), and plain `ssh <alias>`
// from editors and other tools is left unshared, since its config cannot
// tell agents apart.

// ShareEnvVar names the agent socket's key in the control socket path.
const ShareEnvVar = "DCX_SSH_SHARE"

// maxControlPath is the longest control socket path OpenSSH can use: a
// socket path holds at most 104 bytes (macOS; 108 on Linux), and OpenSSH
// appends a 17-character temporary suffix while creating it. A longer
// path fails the whole connection, not just the sharing.
const maxControlPath = 104 - 1 - 17

// controlPersist is how long a shared connection stays open after its
// last session ends.
const controlPersist = "10m"

// ShareEnv returns the environment entry dcx adds to its own ssh commands
// so they use the workspace's shared connection for this SSH agent.
func ShareEnv() string {
	return ShareEnvVar + "=" + agentKey(os.Getenv("SSH_AUTH_SOCK"))
}

// agentKey identifies an agent socket path in a control socket name.
func agentKey(agentSock string) string {
	sum := sha256.Sum256([]byte(agentSock))
	return hex.EncodeToString(sum[:4])
}

// renderShare returns the Match section that shares dcx's connections to
// aliases, "" when the workspace's control socket path would be too long
// for OpenSSH (connections are then made one by one). The socket's folder
// is the workspace folder, which `dcx up` creates first.
func renderShare(workspaceID string, aliases []string) string {
	if workspaceID == "" {
		return ""
	}
	// The longest socket path; ${…} becomes the 8-character agent key.
	longest, err := dcxhome.WorkspacePath(workspaceID, dcxhome.ControlSocketPrefix+agentKey(""))
	if err != nil || len(longest) > maxControlPath {
		return ""
	}
	path := strings.TrimSuffix(longest, agentKey("")) + "${" + ShareEnvVar + "}"
	var b strings.Builder
	// originalhost first: OpenSSH runs exec only when it matched. printenv
	// fails when the variable is unset, whatever the user's shell.
	fmt.Fprintf(&b, "Match originalhost %s exec \"printenv %s\"\n", strings.Join(aliases, ","), ShareEnvVar)
	fmt.Fprintln(&b, "  ControlMaster auto")
	fmt.Fprintf(&b, "  ControlPath %s\n", configPath(path))
	fmt.Fprintf(&b, "  ControlPersist %s\n", controlPersist)
	return b.String()
}
