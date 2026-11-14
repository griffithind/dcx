package helpers

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// agentKeyFingerprint is the SHA256 fingerprint of the key the e2e run's
// SSH agent holds ("" before startSSHAgent).
var agentKeyFingerprint string

// AgentKeyFingerprint returns the fingerprint (as `ssh-add -l` prints it)
// of the one key in the e2e run's SSH agent, so tests can check that the
// forwarded agent is that agent.
func AgentKeyFingerprint() string { return agentKeyFingerprint }

// startSSHAgent serves an in-process SSH agent holding a freshly generated
// ed25519 key on a socket in dir and points SSH_AUTH_SOCK at it. The run
// never depends on (or exposes) the developer's agent, and CI runners,
// which have none, still exercise agent forwarding. The agent stops when
// the returned listener is closed.
func startSSHAgent(dir string) (net.Listener, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv, Comment: "dcx-e2e"}); err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, err
	}

	sock := filepath.Join(dir, "agent.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // closed
			}
			go func() {
				defer conn.Close() //nolint:errcheck // nothing to do on close failure
				_ = agent.ServeAgent(keyring, conn)
			}()
		}
	}()
	if err := os.Setenv("SSH_AUTH_SOCK", sock); err != nil {
		return nil, errors.Join(err, ln.Close())
	}
	agentKeyFingerprint = ssh.FingerprintSHA256(signer.PublicKey())
	return ln, nil
}
