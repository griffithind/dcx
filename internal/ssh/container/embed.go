package container

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"sync"
)

// Embedded gzip-compressed Linux binaries for container deployment.
// These are built and compressed by: make build
//
// The binaries are compressed with gzip to reduce the final binary size.
// They are decompressed on first access and cached in memory.

//go:embed bin/dcx-linux-amd64.gz
var dcxLinuxAmd64Compressed []byte

//go:embed bin/dcx-linux-arm64.gz
var dcxLinuxArm64Compressed []byte

var (
	dcxLinuxAmd64     []byte
	dcxLinuxArm64     []byte
	decompressOnce    sync.Once
	decompressErr     error
	decompressAmd64Ok bool
	decompressArm64Ok bool
)

// GetEmbeddedBinary returns the decompressed Linux binary for the given architecture.
// Returns nil if the architecture is not supported or if binaries are not embedded.
func GetEmbeddedBinary(arch string) ([]byte, error) {
	decompressOnce.Do(func() {
		// Decompress amd64
		if len(dcxLinuxAmd64Compressed) > 0 {
			data, err := decompress(dcxLinuxAmd64Compressed)
			if err == nil {
				dcxLinuxAmd64 = data
				decompressAmd64Ok = true
			}
		}

		// Decompress arm64
		if len(dcxLinuxArm64Compressed) > 0 {
			data, err := decompress(dcxLinuxArm64Compressed)
			if err == nil {
				dcxLinuxArm64 = data
				decompressArm64Ok = true
			}
		}
	})

	switch arch {
	case "amd64", "x86_64":
		if !decompressAmd64Ok {
			return nil, fmt.Errorf("linux/amd64 binary not available")
		}
		return dcxLinuxAmd64, nil
	case "arm64", "aarch64":
		if !decompressArm64Ok {
			return nil, fmt.Errorf("linux/arm64 binary not available")
		}
		return dcxLinuxArm64, nil
	default:
		return nil, fmt.Errorf("unsupported architecture: %s", arch)
	}
}

// HasEmbeddedBinaries returns true if Linux binaries are embedded.
func HasEmbeddedBinaries() bool {
	return len(dcxLinuxAmd64Compressed) > 0 || len(dcxLinuxArm64Compressed) > 0
}

// decompress decompresses gzip-compressed data.
func decompress(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer r.Close()

	decompressed, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress: %w", err)
	}

	return decompressed, nil
}
