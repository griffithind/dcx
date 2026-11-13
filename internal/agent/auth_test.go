package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	gsh "github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// writeTestAuthorizedKeys generates a key pair and writes the public half as
// an authorized_keys file. Returns the path and the matching private signer.
func writeTestAuthorizedKeys(t *testing.T, dir string) (authKeysPath string, signer gossh.Signer) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}

	sshPub, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	authKeys := gossh.MarshalAuthorizedKey(sshPub)

	authKeysPath = filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(authKeysPath, authKeys, 0600); err != nil {
		t.Fatalf("write authorized_keys: %v", err)
	}

	signer, err = gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("NewSignerFromKey: %v", err)
	}
	return authKeysPath, signer
}

func TestAuthorizeAcceptsMatchingKey(t *testing.T) {
	dir := t.TempDir()
	authKeysPath, signer := writeTestAuthorizedKeys(t, dir)

	s := &Server{
		cfg: Config{
			AuthorizedKeysPaths: []string{authKeysPath},
		},
	}
	ctx := fakeSSHContext{user: "nobody"}

	if !s.authorize(ctx, signer.PublicKey()) {
		t.Error("authorize should accept matching key")
	}
}

func TestAuthorizeRejectsDifferentKey(t *testing.T) {
	dir := t.TempDir()
	authKeysPath, _ := writeTestAuthorizedKeys(t, dir)

	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	otherSigner, _ := gossh.NewSignerFromKey(otherPriv)

	s := &Server{
		cfg: Config{
			AuthorizedKeysPaths: []string{authKeysPath},
		},
	}
	ctx := fakeSSHContext{user: "nobody"}

	if s.authorize(ctx, otherSigner.PublicKey()) {
		t.Error("authorize should reject non-matching key")
	}
}

func TestAuthorizeRejectsWhenAuthorizedKeysMissing(t *testing.T) {
	s := &Server{
		cfg: Config{
			AuthorizedKeysPaths: []string{"/nonexistent/path"},
		},
	}
	ctx := fakeSSHContext{user: "nobody"}

	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	sshPub, _ := gossh.NewPublicKey(pub)

	if s.authorize(ctx, sshPub) {
		t.Error("authorize should reject when no authorized_keys file exists")
	}
}

func TestMatchAuthorizedKeyWithComments(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	sshPub, _ := gossh.NewPublicKey(pub)

	data := []byte("# comment line\n" + string(gossh.MarshalAuthorizedKey(sshPub)))
	if !matchAuthorizedKey(data, sshPub) {
		t.Error("matchAuthorizedKey should skip comments")
	}
}

func TestMatchAuthorizedKeyMultiple(t *testing.T) {
	pub1, _, _ := ed25519.GenerateKey(rand.Reader)
	pub2, _, _ := ed25519.GenerateKey(rand.Reader)
	ssh1, _ := gossh.NewPublicKey(pub1)
	ssh2, _ := gossh.NewPublicKey(pub2)

	data := append([]byte{}, gossh.MarshalAuthorizedKey(ssh1)...)
	data = append(data, gossh.MarshalAuthorizedKey(ssh2)...)

	if !matchAuthorizedKey(data, ssh1) {
		t.Error("first entry should match")
	}
	if !matchAuthorizedKey(data, ssh2) {
		t.Error("second entry should match")
	}

	// A third, unrelated key should not match.
	pub3, _, _ := ed25519.GenerateKey(rand.Reader)
	ssh3, _ := gossh.NewPublicKey(pub3)
	if matchAuthorizedKey(data, ssh3) {
		t.Error("unrelated key should not match")
	}
}

func TestSetupHostKeyRequiresPath(t *testing.T) {
	s := &Server{cfg: Config{}}
	srv := &gsh.Server{}
	if err := s.setupHostKey(srv); err == nil {
		t.Error("setupHostKey should error when no HostKeyPath is configured")
	}
}

func TestSetupHostKeyRefusesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "host-key")
	if err := os.WriteFile(bad, []byte("not a key"), 0600); err != nil {
		t.Fatalf("write bad key: %v", err)
	}

	s := &Server{cfg: Config{HostKeyPath: bad}}
	srv := &gsh.Server{}
	if err := s.setupHostKey(srv); err == nil {
		t.Error("setupHostKey should refuse corrupt host key")
	}
}

func TestSetupHostKeyRefusesMissingKey(t *testing.T) {
	// No silent generation: the client pins the key dcx wrote, so a key
	// generated here could never be trusted.
	path := filepath.Join(t.TempDir(), "host-key")
	s := &Server{cfg: Config{HostKeyPath: path}}
	if err := s.setupHostKey(&gsh.Server{}); err == nil {
		t.Fatal("setupHostKey should refuse a missing host key")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("setupHostKey must not create a host key")
	}
}

func TestSetupHostKeyLoadsKey(t *testing.T) {
	s := &Server{cfg: Config{HostKeyPath: writeTestHostKey(t, t.TempDir())}}
	if err := s.setupHostKey(&gsh.Server{}); err != nil {
		t.Fatalf("setupHostKey: %v", err)
	}
}

// writeTestHostKey writes an ed25519 host key in OpenSSH PEM form.
func writeTestHostKey(t *testing.T, dir string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := gossh.MarshalPrivateKey(priv, "test host key")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "host-key")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeSSHContext satisfies just enough of ssh.Context for authorize's needs.
type fakeSSHContext struct {
	gsh.Context
	user string
}

func (c fakeSSHContext) User() string { return c.user }
