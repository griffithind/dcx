// Package dcxembed provides embedded dcx-agent binaries for container deployment.
package dcxembed

import (
	"bytes"
	"compress/gzip"
	"embed"
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

var (
	binaries   = map[string][]byte{}
	binaryErrs = map[string]error{}
	loadOnce   sync.Once
)

// Binary returns the decompressed Linux agent binary for the given architecture.
func Binary(arch string) ([]byte, error) {
	loadOnce.Do(func() {
		for _, a := range []string{"amd64", "arm64"} {
			binaries[a], binaryErrs[a] = load(a)
		}
	})
	switch arch {
	case "amd64", "x86_64":
		arch = "amd64"
	case "arm64", "aarch64":
		arch = "arm64"
	default:
		return nil, fmt.Errorf("unsupported architecture: %s", arch)
	}
	if err := binaryErrs[arch]; err != nil {
		return nil, err
	}
	return binaries[arch], nil
}

func load(arch string) ([]byte, error) {
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
	return decompress(compressed)
}

func decompress(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer r.Close() //nolint:errcheck // Close error irrelevant after read
	return io.ReadAll(r)
}
