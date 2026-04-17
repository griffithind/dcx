package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

func TestListenAcceptsAuthorizedKey(t *testing.T) {
	dir := t.TempDir()
	authKeysPath, signer := writeTestAuthorizedKeys(t, dir)

	readyFile := filepath.Join(dir, "ready")
	srv, err := NewServer(Config{
		Shell:               "/bin/sh",
		WorkDir:             dir,
		HostKeyPath:         filepath.Join(dir, "host-key"),
		AuthorizedKeysPaths: []string{authKeysPath},
		Gate:                Loopback(),
		ReadyFile:           readyFile,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listenDone := make(chan error, 1)
	go func() {
		// Use a kernel-assigned port by asking for :0. We learn the actual
		// port via the ready file below.
		listenDone <- srv.Listen(ctx, "127.0.0.1:0")
	}()

	// Wait until the ready file exists to learn the bound port.
	port := waitForReadyAddr(t, readyFile, 2*time.Second)

	// Dial and complete an SSH handshake.
	clientCfg := &gossh.ClientConfig{
		User: os.Getenv("USER"),
		Auth: []gossh.AuthMethod{
			gossh.PublicKeys(signer),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         2 * time.Second,
	}
	client, err := gossh.Dial("tcp", port, clientCfg)
	if err != nil {
		t.Fatalf("gossh.Dial: %v", err)
	}

	// Close the client before cancelling, otherwise Shutdown waits up to 10s
	// for the session to drain.
	_ = client.Close()
	// Give the server a moment to reap the connection so Shutdown returns
	// immediately rather than ticking through its 10s drain timeout.
	time.Sleep(50 * time.Millisecond)

	// Cancel triggers graceful shutdown.
	cancel()
	select {
	case err := <-listenDone:
		if err != nil {
			t.Errorf("Listen returned error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Error("Listen did not shut down within 15s")
	}
}

func TestListenRejectsUnauthorizedKey(t *testing.T) {
	dir := t.TempDir()
	authKeysPath, _ := writeTestAuthorizedKeys(t, dir)

	// Generate a *different* key pair for the client.
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	otherSigner, _ := gossh.NewSignerFromKey(otherPriv)

	readyFile := filepath.Join(dir, "ready")
	srv, err := NewServer(Config{
		Shell:               "/bin/sh",
		WorkDir:             dir,
		HostKeyPath:         filepath.Join(dir, "host-key"),
		AuthorizedKeysPaths: []string{authKeysPath},
		Gate:                Loopback(),
		ReadyFile:           readyFile,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = srv.Listen(ctx, "127.0.0.1:0") }()
	addr := waitForReadyAddr(t, readyFile, 2*time.Second)

	clientCfg := &gossh.ClientConfig{
		User: "nobody",
		Auth: []gossh.AuthMethod{
			gossh.PublicKeys(otherSigner),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         2 * time.Second,
	}
	_, err = gossh.Dial("tcp", addr, clientCfg)
	if err == nil {
		t.Error("Dial with unauthorized key should have failed")
	}
}

func TestNewServerRequiresGate(t *testing.T) {
	_, err := NewServer(Config{
		Shell:               "/bin/sh",
		WorkDir:             "/tmp",
		HostKeyPath:         filepath.Join(t.TempDir(), "hk"),
		AuthorizedKeysPaths: []string{filepath.Join(t.TempDir(), "auth")},
	})
	if err == nil {
		t.Error("NewServer without Gate should error")
	}
}

func TestNewServerRequiresAuthorizedKeys(t *testing.T) {
	_, err := NewServer(Config{
		Shell:       "/bin/sh",
		WorkDir:     "/tmp",
		HostKeyPath: filepath.Join(t.TempDir(), "hk"),
		Gate:        Loopback(),
	})
	if err == nil {
		t.Error("NewServer without AuthorizedKeysPaths should error")
	}
}

// waitForReadyAddr polls the ready file until it contains a bound address.
func waitForReadyAddr(t *testing.T, readyFile string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(readyFile)
		if err == nil {
			addr := string(data)
			// Strip trailing newline.
			if len(addr) > 0 && addr[len(addr)-1] == '\n' {
				addr = addr[:len(addr)-1]
			}
			// Verify it's actually listening before returning.
			conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return addr
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("listener did not become ready within %v", timeout)
	return ""
}
