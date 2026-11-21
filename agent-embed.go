// Package dcxembed provides embedded dcx-agent binaries for container deployment.
package dcxembed

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"
)

// agentFS holds the gzip-compressed Linux agent binaries `make build-agent`
// writes to agentbin/. The directory is committed (with a README) so the
// module compiles on a clean checkout; the binaries themselves are not.
//
//go:embed agentbin
var agentFS embed.FS

// Digest identifies the agent binary for arch: the SHA-256 of its
// compressed form, which is cheap to compute (no decompression) and
// changes with the binary. The Makefile compresses with `gzip -n` (no
// name or time in the header), so rebuilding an identical binary keeps
// the digest and running containers keep their agent.
func Digest(arch string) (string, error) {
	arch, err := goArch(arch)
	if err != nil {
		return "", err
	}
	compressed, err := compressedBinary(arch)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(compressed)
	return hex.EncodeToString(sum[:]), nil
}

// Binary returns the decompressed Linux agent binary for the given
// architecture, decompressing it once per process.
func Binary(arch string) ([]byte, error) {
	arch, err := goArch(arch)
	if err != nil {
		return nil, err
	}
	once, _ := decompressed.LoadOrStore(arch, sync.OnceValues(func() ([]byte, error) {
		compressed, err := compressedBinary(arch)
		if err != nil {
			return nil, err
		}
		return decompress(compressed)
	}))
	return once.(func() ([]byte, error))()
}

// decompressed holds each architecture's decompression (func() ([]byte,
// error) from sync.OnceValues), so a background Binary call started early
// is not repeated.
var decompressed sync.Map

// goArch maps uname and Docker architecture names to Go's.
func goArch(arch string) (string, error) {
	switch arch {
	case "amd64", "x86_64":
		return "amd64", nil
	case "arm64", "aarch64":
		return "arm64", nil
	}
	return "", fmt.Errorf("unsupported architecture: %s", arch)
}

func compressedBinary(arch string) ([]byte, error) {
	compressed, err := agentFS.ReadFile("agentbin/dcx-agent-linux-" + arch + ".gz")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("linux/%s agent binary not embedded: build dcx with `make build` (it runs `make build-agent` first)", arch)
	}
	if err != nil {
		return nil, err
	}
	if len(compressed) == 0 {
		return nil, fmt.Errorf("linux/%s agent binary is empty: rebuild with `make build`", arch)
	}
	return compressed, nil
}

func decompress(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer r.Close() //nolint:errcheck // Close error irrelevant after read
	return io.ReadAll(r)
}
