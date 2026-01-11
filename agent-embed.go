// Package dcxembed provides embedded dcx-agent binaries for container deployment.
package dcxembed

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"sync"
)

// Embedded gzip-compressed Linux agent binaries.
// Built by: make build-agent

//go:embed bin/dcx-agent-linux-amd64.gz
var agentLinuxAmd64Compressed []byte

//go:embed bin/dcx-agent-linux-arm64.gz
var agentLinuxArm64Compressed []byte

var (
	agentLinuxAmd64   []byte
	agentLinuxArm64   []byte
	decompressOnce    sync.Once
	decompressAmd64Ok bool
	decompressArm64Ok bool
)

// GetBinary returns the decompressed Linux agent binary for the given architecture.
func GetBinary(arch string) ([]byte, error) {
	decompressOnce.Do(func() {
		if len(agentLinuxAmd64Compressed) > 0 {
			if data, err := decompress(agentLinuxAmd64Compressed); err == nil {
				agentLinuxAmd64 = data
				decompressAmd64Ok = true
			}
		}
		if len(agentLinuxArm64Compressed) > 0 {
			if data, err := decompress(agentLinuxArm64Compressed); err == nil {
				agentLinuxArm64 = data
				decompressArm64Ok = true
			}
		}
	})

	switch arch {
	case "amd64", "x86_64":
		if !decompressAmd64Ok {
			return nil, fmt.Errorf("linux/amd64 agent binary not available")
		}
		return agentLinuxAmd64, nil
	case "arm64", "aarch64":
		if !decompressArm64Ok {
			return nil, fmt.Errorf("linux/arm64 agent binary not available")
		}
		return agentLinuxArm64, nil
	default:
		return nil, fmt.Errorf("unsupported architecture: %s", arch)
	}
}

// HasBinaries returns true if agent binaries are embedded.
func HasBinaries() bool {
	return len(agentLinuxAmd64Compressed) > 0 || len(agentLinuxArm64Compressed) > 0
}

func decompress(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer r.Close() //nolint:errcheck // Close error irrelevant after read
	return io.ReadAll(r)
}
