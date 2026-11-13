package ssh

// authkeys.go collects the public keys the in-container agent accepts.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	gossh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// AuthorizedKeys builds the authorized_keys list the agent installs
// as /run/secrets/dcx/authorized_keys. All of the following sources are
// merged unconditionally so clients have multiple paths to authenticate:
//
//  1. Every *.pub file under ~/.ssh/ (covers id_ed25519, id_ecdsa, id_rsa,
//     and the "I have multiple" case some users have).
//  2. Identities exposed by the SSH agent at $SSH_AUTH_SOCK (handles
//     yubikey / 1Password / Secretive users who keep no key on disk).
//  3. The dcx fallback key at ~/.dcx/id_ed25519, auto-generated on first
//     use. This is ALWAYS included — some clients (Claude Desktop's ssh2,
//     JetBrains Gateway with a manual private-key field) can only point
//     at a file path and cannot reach the local SSH agent. Without the
//     fallback in authorized_keys, those clients get "All configured
//     authentication methods failed" even though OpenSSH succeeds via
//     the agent.
//
// Duplicate pubkeys across sources (e.g. the same key on disk and in the
// agent) are fine — `ssh.ParseAuthorizedKey` processes them one at a time
// and `ssh.KeysEqual` is cheap.
func AuthorizedKeys() ([]byte, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	var keys []byte
	appendBlock := func(block []byte) {
		if len(block) == 0 {
			return
		}
		keys = append(keys, block...)
		if block[len(block)-1] != '\n' {
			keys = append(keys, '\n')
		}
	}

	sshDir := filepath.Join(home, ".ssh")
	entries, _ := os.ReadDir(sshDir)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".pub") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sshDir, name))
		if err != nil {
			continue
		}
		appendBlock(data)
	}

	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if agentKeys, err := readAgentPubkeys(sock); err == nil {
			appendBlock(agentKeys)
		}
	}

	// Always include the dcx fallback. Generated on first call, then re-read
	// from disk on subsequent invocations.
	if fallback, err := ensureFallbackClientKey(home); err == nil {
		appendBlock(fallback)
	} else if len(keys) == 0 {
		return nil, fmt.Errorf("no SSH public keys found and fallback generation failed: %w", err)
	}
	return keys, nil
}

// ensureFallbackClientKey returns the public key half of ~/.dcx/id_ed25519,
// generating the key pair on first call.
func ensureFallbackClientKey(home string) ([]byte, error) {
	dir := filepath.Join(home, ".dcx")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create ~/.dcx: %w", err)
	}

	privPath := filepath.Join(dir, "id_ed25519")
	pubPath := privPath + ".pub"

	// Fast path: both already exist.
	if pub, err := os.ReadFile(pubPath); err == nil {
		if _, err := os.Stat(privPath); err == nil {
			return pub, nil
		}
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate fallback key: %w", err)
	}

	// Write the private key in OpenSSH format.
	pemBlock, err := gossh.MarshalPrivateKey(priv, "dcx fallback client key")
	if err != nil {
		return nil, fmt.Errorf("marshal fallback private key: %w", err)
	}
	pemBytes := pem.EncodeToMemory(pemBlock)
	if err := os.WriteFile(privPath, pemBytes, 0600); err != nil {
		return nil, fmt.Errorf("write fallback private key: %w", err)
	}

	sshPub, err := gossh.NewPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("public key from fallback: %w", err)
	}
	authLine := gossh.MarshalAuthorizedKey(sshPub)
	if err := os.WriteFile(pubPath, authLine, 0644); err != nil {
		return nil, fmt.Errorf("write fallback public key: %w", err)
	}
	return authLine, nil
}

// readAgentPubkeys connects to the SSH agent and returns its identities
// marshalled as authorized_keys lines.
func readAgentPubkeys(sock string) ([]byte, error) {
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	client := sshagent.NewClient(conn)
	list, err := client.List()
	if err != nil {
		return nil, err
	}

	var out []byte
	for _, k := range list {
		out = append(out, gossh.MarshalAuthorizedKey(k)...)
	}
	return out, nil
}
